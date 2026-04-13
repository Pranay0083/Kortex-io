package main

import (
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// --- system / ops --------------------------------------------------------

func (a *App) handleSystemStats(w http.ResponseWriter, r *http.Request) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	writeJSON(w, 200, map[string]any{
		"cache":      a.cache.Stats(r.Context()),
		"bus":        a.bus.Stats(),
		"olap":       a.olap.Stats(),
		"bloom":      a.bloom.Stats(),
		"shards":     a.store.ShardDistribution(r.Context()),
		"pg_pool":    a.store.PoolStats(),
		"id_gen":     map[string]any{"issued": a.sf.Issued.Load(), "seq_exhausted": a.sf.SeqStall.Load(), "clock_backwards": a.sf.ClockBk.Load(), "node_id": a.cfg.NodeID},
		"runtime":    map[string]any{"goroutines": runtime.NumGoroutine(), "heap_mb": float64(m.HeapAlloc) / 1024 / 1024, "gc_cycles": m.NumGC, "cpus": runtime.NumCPU(), "go_version": runtime.Version()},
		"uptime_s":   int(time.Since(a.started).Seconds()),
		"redirects":  a.redirects.Load(),
		"node":       "kortex-edge-01",
	})
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"status": "ok", "service": "kortex", "runtime": runtime.Version(),
		"postgres": a.store.pool.Ping(r.Context()) == nil,
		"uptime_s": int(time.Since(a.started).Seconds()),
	})
}

// --- lab: snowflake ------------------------------------------------------

func (a *App) handleDecodeID(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if code := strings.TrimSpace(q.Get("code")); code != "" {
		id, err := decodeCode(code)
		if err != nil {
			writeErr(w, 400, "not a base62 code")
			return
		}
		p := explodeID(id)
		ok := p.TimestampMS > kortexEpoch && p.TimestampMS < time.Now().Add(time.Hour).UnixMilli()
		writeJSON(w, 200, map[string]any{"parts": p, "plausible": ok, "input": code})
		return
	}
	if raw := strings.TrimSpace(q.Get("id")); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeErr(w, 400, "id must be an integer")
			return
		}
		writeJSON(w, 200, map[string]any{"parts": explodeID(id), "plausible": true, "input": raw})
		return
	}
	// no input: mint a fresh one
	id := a.sf.Next()
	writeJSON(w, 200, map[string]any{"parts": explodeID(id), "plausible": true, "input": "generated"})
}

// Burst test: how many unique IDs can one node mint, and do they collide?
func (a *App) handleIDBurst(w http.ResponseWriter, r *http.Request) {
	n := qInt(r, "n", 50000)
	if n > 500000 {
		n = 500000
	}
	seen := make(map[int64]struct{}, n)
	sf := NewSnowflake(a.cfg.NodeID)
	start := time.Now()
	collisions := 0
	sample := []SnowflakeParts{}
	for i := 0; i < n; i++ {
		id := sf.Next()
		if _, dup := seen[id]; dup {
			collisions++
		}
		seen[id] = struct{}{}
		if i < 6 {
			sample = append(sample, explodeID(id))
		}
	}
	el := time.Since(start)
	writeJSON(w, 200, map[string]any{
		"generated": n, "unique": len(seen), "collisions": collisions,
		"elapsed_ms": el.Milliseconds(), "ids_per_sec": float64(n) / el.Seconds(),
		"seq_exhausted": sf.SeqStall.Load(), "sample": sample,
		"theoretical_max_per_sec": (maxSeq + 1) * 1000,
	})
}

// --- lab: consistent hashing --------------------------------------------

func (a *App) handleRing(w http.ResponseWriter, r *http.Request) {
	from := qInt(r, "from", a.cfg.ShardCount)
	to := qInt(r, "to", a.cfg.ShardCount+1)
	vnodes := qInt(r, "vnodes", a.cfg.VNodes)
	samples := qInt(r, "samples", 20000)
	if from < 1 {
		from = 1
	}
	if to < 1 {
		to = 1
	}
	if samples > 200000 {
		samples = 200000
	}
	writeJSON(w, 200, ringReport(from, to, vnodes, samples, nil))
}

// --- lab: bloom playground ----------------------------------------------

func (a *App) handleBloomState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"stats":   a.labBloom.Stats(),
		"preview": a.labBloom.Preview(256),
		"keys":    a.labKeys,
	})
}

func (a *App) handleBloomReset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Expected int     `json:"expected"`
		FPRate   float64 `json:"fp_rate"`
	}
	_ = readJSON(r, &body)
	if body.Expected <= 0 {
		body.Expected = 1000
	}
	if body.FPRate <= 0 || body.FPRate >= 1 {
		body.FPRate = 0.01
	}
	a.labBloom = NewBloom(body.Expected, body.FPRate)
	a.labKeys = []string{}
	writeJSON(w, 200, map[string]any{"stats": a.labBloom.Stats(), "preview": a.labBloom.Preview(256), "keys": a.labKeys})
}

func (a *App) handleBloomAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key string `json:"key"`
	}
	if readJSON(r, &body) != nil || strings.TrimSpace(body.Key) == "" {
		writeErr(w, 400, "key required")
		return
	}
	k := strings.TrimSpace(body.Key)
	a.labBloom.Add(k)
	a.labKeys = append(a.labKeys, k)
	if len(a.labKeys) > 40 {
		a.labKeys = a.labKeys[len(a.labKeys)-40:]
	}
	writeJSON(w, 200, map[string]any{
		"added": k, "bits": a.labBloom.SetBits(k),
		"stats": a.labBloom.Stats(), "preview": a.labBloom.Preview(256), "keys": a.labKeys,
	})
}

func (a *App) handleBloomTest(w http.ResponseWriter, r *http.Request) {
	k := strings.TrimSpace(r.URL.Query().Get("key"))
	if k == "" {
		writeErr(w, 400, "key required")
		return
	}
	maybe := a.labBloom.MayContain(k)
	truth := false
	for _, e := range a.labKeys {
		if e == k {
			truth = true
			break
		}
	}
	verdict := "definitely not in set"
	if maybe && truth {
		verdict = "in set (true positive)"
	} else if maybe && !truth {
		verdict = "FALSE POSITIVE — bloom says maybe, set says no"
	}
	writeJSON(w, 200, map[string]any{
		"key": k, "maybe": maybe, "actually_present": truth, "verdict": verdict,
		"bits": a.labBloom.SetBits(k), "stats": a.labBloom.Stats(),
	})
}

// Measures the real false-positive rate by hammering the filter with misses.
func (a *App) handleBloomSweep(w http.ResponseWriter, r *http.Request) {
	inserts := qInt(r, "inserts", 5000)
	probes := qInt(r, "probes", 20000)
	fp := r.URL.Query().Get("fp_rate")
	rate := 0.01
	if fp != "" {
		if v, err := strconv.ParseFloat(fp, 64); err == nil && v > 0 && v < 1 {
			rate = v
		}
	}
	if inserts > 200000 {
		inserts = 200000
	}
	if probes > 500000 {
		probes = 500000
	}
	b := NewBloom(inserts, rate)
	for i := 0; i < inserts; i++ {
		b.Add("member-" + strconv.Itoa(i))
	}
	start := time.Now()
	falsePos := 0
	for i := 0; i < probes; i++ {
		if b.MayContain("outsider-" + strconv.Itoa(i)) {
			falsePos++
		}
	}
	el := time.Since(start)
	st := b.Stats()
	writeJSON(w, 200, map[string]any{
		"inserts": inserts, "probes": probes, "false_positives": falsePos,
		"measured_fpr": float64(falsePos) / float64(probes),
		"predicted_fpr": st.EstFPR, "target_fpr": rate,
		"m": st.M, "k": st.K, "memory_kb": st.MemoryKB,
		"lookup_ns": el.Nanoseconds() / int64(probes),
	})
}

func (a *App) rngPool() rnd { return fastRand{} }
