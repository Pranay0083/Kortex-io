package main

// Ops and "lab" endpoints. /api/system/stats feeds the ops dashboard. The
// lab endpoints let you play with the Snowflake IDs, consistent hash ring
// and Bloom filter interactively. All are public; the only shared state is
// the lab's toy Bloom filter.

import (
	"net/http"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

// --- system / ops --------------------------------------------------------

func (a *App) handleSystemStats(w http.ResponseWriter, r *http.Request) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	writeJSON(w, http.StatusOK, map[string]any{
		"cache":       a.cache.Stats(r.Context()),
		"bus":         a.bus.Stats(),
		"olap":        a.olap.Stats(),
		"bloom":       a.bloom.Stats(),
		"shards":      a.store.ShardDistribution(r.Context()),
		"shard_count": a.cfg.ShardCount,
		"pg_pool":     a.store.PoolStats(),
		"id_gen": map[string]any{
			"issued": a.ids.Issued.Load(), "seq_exhausted": a.ids.SeqExhausted.Load(),
			"clock_backwards": a.ids.ClockBackwards.Load(), "node_id": a.cfg.NodeID,
		},
		"runtime": map[string]any{
			"goroutines": runtime.NumGoroutine(), "heap_mb": float64(m.HeapAlloc) / 1024 / 1024,
			"gc_cycles": m.NumGC, "cpus": runtime.NumCPU(), "go_version": runtime.Version(),
		},
		"uptime_s":      int(time.Since(a.started).Seconds()),
		"redirects":     a.redirects.Load(),
		"unknown_codes": a.unknownCodes.Load(),
		"node_name":     a.cfg.NodeName,
		"node":          a.cfg.NodeName,
		"demo_mode":     a.cfg.DemoMode,
	})
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok", "service": "kortex", "runtime": runtime.Version(),
		"postgres": a.store.pool.Ping(r.Context()) == nil,
		"uptime_s": int(time.Since(a.started).Seconds()),
	})
}

// --- lab: snowflake ------------------------------------------------------

// handleDecodeID splits an ID into its fields. Input is ?code= (a short
// code), ?id= (a raw ID), or nothing (mint a new one).
func (a *App) handleDecodeID(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if code := strings.TrimSpace(q.Get("code")); code != "" {
		id, err := decodeCode(code)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "not a base62 code")
			return
		}
		p := explodeID(id)
		// Any valid base62 string decodes to *some* ID; only call it
		// plausible if its timestamp is between the epoch and now.
		plausible := p.TimestampMS > kortexEpoch && p.TimestampMS < time.Now().Add(time.Hour).UnixMilli()
		writeJSON(w, http.StatusOK, map[string]any{"parts": p, "plausible": plausible, "input": code})
		return
	}
	if raw := strings.TrimSpace(q.Get("id")); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "id must be an integer")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"parts": explodeID(id), "plausible": true, "input": raw})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"parts": explodeID(a.ids.Next()), "plausible": true, "input": "generated"})
}

// handleIDBurst mints n IDs as fast as it can on a private generator and
// checks for collisions and throughput.
func (a *App) handleIDBurst(w http.ResponseWriter, r *http.Request) {
	n := clamp(qInt(r, "n", 50000), 1, 500000)
	gen := NewSnowflake(a.cfg.NodeID)
	seen := make(map[int64]struct{}, n)
	collisions := 0
	sample := []SnowflakeParts{}
	start := time.Now()
	for i := 0; i < n; i++ {
		id := gen.Next()
		if _, dup := seen[id]; dup {
			collisions++
		}
		seen[id] = struct{}{}
		if i < 6 {
			sample = append(sample, explodeID(id))
		}
	}
	elapsed := time.Since(start)
	writeJSON(w, http.StatusOK, map[string]any{
		"generated": n, "unique": len(seen), "collisions": collisions,
		"elapsed_ms": elapsed.Milliseconds(), "ids_per_sec": float64(n) / elapsed.Seconds(),
		"seq_exhausted": gen.SeqExhausted.Load(), "sample": sample,
		"theoretical_max_per_sec": (maxSeq + 1) * 1000,
	})
}

// --- lab: consistent hashing --------------------------------------------

func (a *App) handleRing(w http.ResponseWriter, r *http.Request) {
	from := clamp(qInt(r, "from", a.cfg.ShardCount), 1, 64)
	to := clamp(qInt(r, "to", a.cfg.ShardCount+1), 1, 64)
	vnodes := clamp(qInt(r, "vnodes", a.cfg.VNodes), 1, 1000)
	samples := clamp(qInt(r, "samples", 20000), 1, 200000)
	writeJSON(w, http.StatusOK, ringReport(from, to, vnodes, samples, nil))
}

// --- lab: bloom playground ----------------------------------------------

// Every visitor shares the playground filter. labMu guards replacing it and
// the list of recent keys (that list is the truth used to spot false
// positives).

const (
	labMaxExpected = 100000
	labMinFPRate   = 1e-6
	labMaxKeys     = 40
	labMaxKeyLen   = 256
)

// labState snapshots the playground. Caller holds labMu.
func (a *App) labState() map[string]any {
	return map[string]any{
		"stats":   a.labBloom.Stats(),
		"preview": a.labBloom.Preview(256),
		"keys":    slices.Clone(a.labKeys),
	}
}

func (a *App) handleBloomState(w http.ResponseWriter, r *http.Request) {
	a.labMu.Lock()
	out := a.labState()
	a.labMu.Unlock()
	writeJSON(w, http.StatusOK, out)
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
	b := NewBloom(min(body.Expected, labMaxExpected), max(body.FPRate, labMinFPRate))

	a.labMu.Lock()
	a.labBloom = b
	a.labKeys = []string{}
	out := a.labState()
	a.labMu.Unlock()
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleBloomAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key string `json:"key"`
	}
	if readJSON(r, &body) != nil || strings.TrimSpace(body.Key) == "" {
		writeErr(w, http.StatusBadRequest, "key required")
		return
	}
	k := strings.TrimSpace(body.Key)
	if len(k) > labMaxKeyLen {
		writeErr(w, http.StatusBadRequest, "key must be at most 256 bytes")
		return
	}
	a.labMu.Lock()
	a.labBloom.Add(k)
	a.labKeys = append(a.labKeys, k)
	if len(a.labKeys) > labMaxKeys {
		a.labKeys = a.labKeys[len(a.labKeys)-labMaxKeys:]
	}
	out := a.labState()
	out["added"], out["bits"] = k, a.labBloom.SetBits(k)
	a.labMu.Unlock()
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleBloomTest(w http.ResponseWriter, r *http.Request) {
	k := strings.TrimSpace(r.URL.Query().Get("key"))
	if k == "" {
		writeErr(w, http.StatusBadRequest, "key required")
		return
	}
	a.labMu.Lock()
	b := a.labBloom
	present := slices.Contains(a.labKeys, k)
	a.labMu.Unlock()

	maybe := b.MayContain(k)
	verdict := "definitely not in set"
	switch {
	case maybe && present:
		verdict = "in set (true positive)"
	case maybe:
		verdict = "FALSE POSITIVE — bloom says maybe, set says no"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"key": k, "maybe": maybe, "actually_present": present, "verdict": verdict,
		"bits": b.SetBits(k), "stats": b.Stats(),
	})
}

// handleBloomSweep builds a fresh filter, inserts members, then probes with
// keys that were never inserted. Every "maybe" is a false positive, so this
// measures the real rate and compares it with the formula.
func (a *App) handleBloomSweep(w http.ResponseWriter, r *http.Request) {
	inserts := clamp(qInt(r, "inserts", 5000), 1, 200000)
	probes := clamp(qInt(r, "probes", 20000), 1, 500000)
	rate := 0.01
	if v, err := strconv.ParseFloat(r.URL.Query().Get("fp_rate"), 64); err == nil && v > 0 && v < 1 {
		rate = max(v, labMinFPRate)
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
	elapsed := time.Since(start)
	st := b.Stats()
	writeJSON(w, http.StatusOK, map[string]any{
		"inserts": inserts, "probes": probes, "false_positives": falsePos,
		"measured_fpr":  float64(falsePos) / float64(probes),
		"predicted_fpr": st.EstFPR, "target_fpr": rate,
		"m": st.M, "k": st.K, "memory_kb": st.MemoryKB,
		"lookup_ns": elapsed.Nanoseconds() / int64(probes),
	})
}
