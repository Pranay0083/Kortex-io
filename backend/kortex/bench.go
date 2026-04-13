package main

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const histBuckets = 40000 // 10us resolution up to 400ms

type Histogram struct {
	buckets []atomic.Int64
	count   atomic.Int64
	sum     atomic.Int64 // microseconds
	max     atomic.Int64
}

func NewHistogram() *Histogram {
	return &Histogram{buckets: make([]atomic.Int64, histBuckets+1)}
}

func (h *Histogram) Observe(us int64) {
	i := us / 10
	if i > histBuckets {
		i = histBuckets
	}
	h.buckets[i].Add(1)
	h.count.Add(1)
	h.sum.Add(us)
	for {
		cur := h.max.Load()
		if us <= cur || h.max.CompareAndSwap(cur, us) {
			break
		}
	}
}

func (h *Histogram) Percentile(p float64) float64 {
	total := h.count.Load()
	if total == 0 {
		return 0
	}
	target := int64(float64(total) * p)
	var acc int64
	for i := 0; i <= histBuckets; i++ {
		acc += h.buckets[i].Load()
		if acc >= target {
			return float64(i*10) / 1000.0
		}
	}
	return float64(h.max.Load()) / 1000.0
}

func (h *Histogram) Mean() float64 {
	c := h.count.Load()
	if c == 0 {
		return 0
	}
	return float64(h.sum.Load()) / float64(c) / 1000.0
}

type BenchSnapshot struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	Running      bool     `json:"running"`
	Phase        string   `json:"phase"`
	StartedAt    int64    `json:"started_at"`
	DurationS    int      `json:"duration_s"`
	Concurrency  int      `json:"concurrency"`
	Elapsed      float64  `json:"elapsed_s"`
	Progress     float64  `json:"progress"`
	Requests     int64    `json:"requests"`
	Errors       int64    `json:"errors"`
	RPS          float64  `json:"rps"`
	PeakRPS      float64  `json:"peak_rps"`
	Mean         float64  `json:"mean_ms"`
	P50          float64  `json:"p50_ms"`
	P90          float64  `json:"p90_ms"`
	P95          float64  `json:"p95_ms"`
	P99          float64  `json:"p99_ms"`
	Max          float64  `json:"max_ms"`
	CacheHitRate float64  `json:"cache_hit_rate"`
	CacheHits    uint64   `json:"cache_hits"`
	CacheMiss    uint64   `json:"cache_misses"`
	EventsQueued uint64   `json:"events_queued"`
	EventsStored uint64   `json:"events_stored"`
	IngestRate   float64  `json:"ingest_rate"`
	Log          []string `json:"log"`
	Series       []BenchTick `json:"series"`
}

type BenchTick struct {
	T   float64 `json:"t"`
	RPS float64 `json:"rps"`
	P99 float64 `json:"p99"`
}

type Bench struct {
	mu      sync.Mutex
	snap    BenchSnapshot
	hist    *Histogram
	running atomic.Bool
	reqs    atomic.Int64
	errs    atomic.Int64
	cancel  context.CancelFunc
}

func NewBench() *Bench { return &Bench{hist: NewHistogram(), snap: BenchSnapshot{Phase: "idle"}} }

func (b *Bench) Snapshot() BenchSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.snap
	s.Requests = b.reqs.Load()
	s.Errors = b.errs.Load()
	return s
}

func (b *Bench) log(format string, args ...any) {
	b.mu.Lock()
	b.snap.Log = append(b.snap.Log, fmt.Sprintf("%-8s %s", time.Now().UTC().Format("15:04:05"), fmt.Sprintf(format, args...)))
	if len(b.snap.Log) > 120 {
		b.snap.Log = b.snap.Log[len(b.snap.Log)-120:]
	}
	b.mu.Unlock()
}

type benchReq struct {
	Duration    int    `json:"duration_s"`
	Concurrency int    `json:"concurrency"`
	Kind        string `json:"kind"`
	Events      int    `json:"events"`
}

func (a *App) handleBenchStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.bench.Snapshot())
}

func (a *App) handleBenchStop(w http.ResponseWriter, r *http.Request) {
	a.bench.mu.Lock()
	if a.bench.cancel != nil {
		a.bench.cancel()
	}
	a.bench.mu.Unlock()
	writeJSON(w, 200, map[string]bool{"stopping": true})
}

func (a *App) handleBenchStart(w http.ResponseWriter, r *http.Request) {
	var req benchReq
	_ = readJSON(r, &req)
	if a.bench.running.Load() {
		writeErr(w, 409, "a benchmark is already running")
		return
	}
	if req.Duration <= 0 || req.Duration > 60 {
		req.Duration = 10
	}
	if req.Concurrency <= 0 || req.Concurrency > 512 {
		req.Concurrency = 64
	}
	if req.Events <= 0 || req.Events > 5000000 {
		req.Events = 1000000
	}
	if req.Kind == "" {
		req.Kind = "redirect"
	}
	codes := a.benchCodes(r.Context())
	if len(codes) == 0 {
		writeErr(w, 400, "no links to benchmark — create one first")
		return
	}
	a.bench.running.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	a.bench.mu.Lock()
	a.bench.hist = NewHistogram()
	a.bench.reqs.Store(0)
	a.bench.errs.Store(0)
	a.bench.cancel = cancel
	a.bench.snap = BenchSnapshot{
		ID: strconv.FormatInt(time.Now().UnixNano(), 36), Kind: req.Kind, Running: true,
		Phase: "warming", StartedAt: time.Now().UnixMilli(), DurationS: req.Duration,
		Concurrency: req.Concurrency, Log: []string{}, Series: []BenchTick{},
	}
	a.bench.mu.Unlock()

	if req.Kind == "ingest" {
		go a.runIngestBench(ctx, req.Events, codes)
	} else {
		go a.runRedirectBench(ctx, req, codes)
	}
	writeJSON(w, 202, a.bench.Snapshot())
}

func (a *App) benchCodes(ctx context.Context) []string {
	links, err := a.store.AllCodes(ctx)
	if err != nil {
		return nil
	}
	out := []string{}
	for _, l := range links {
		if l.Active {
			out = append(out, l.Code)
		}
		if len(out) >= 200 {
			break
		}
	}
	return out
}

func (a *App) runRedirectBench(ctx context.Context, req benchReq, codes []string) {
	b := a.bench
	defer func() {
		b.running.Store(false)
		b.mu.Lock()
		b.snap.Running = false
		b.snap.Phase = "done"
		b.snap.Progress = 1
		b.mu.Unlock()
	}()

	base := "http://127.0.0.1:" + a.cfg.Port + "/api/r/"
	transport := &http.Transport{
		MaxIdleConns:        req.Concurrency * 2,
		MaxIdleConnsPerHost: req.Concurrency * 2,
		MaxConnsPerHost:     req.Concurrency * 2,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  true,
		DialContext:         (&net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	}
	client := &http.Client{
		Transport:     transport,
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	b.log("k6-style loopback runner: %d VUs -> %s{code}", req.Concurrency, base)
	b.log("target pool: %d active short codes across %d shards", len(codes), a.cfg.ShardCount)
	b.log("warming connection pool + redis working set ...")

	// warm-up: prime keep-alive connections and the Redis working set
	for i := 0; i < len(codes) && i < 64; i++ {
		rq, _ := http.NewRequest("GET", base+codes[i], nil)
		rq.Header.Set("User-Agent", "k6/kortex-warmup")
		if resp, err := client.Do(rq); err == nil {
			resp.Body.Close()
		}
	}

	h0, m0 := a.cache.Hits.Load(), a.cache.Misses.Load()
	start := time.Now()
	deadline := start.Add(time.Duration(req.Duration) * time.Second)

	b.mu.Lock()
	b.snap.Phase = "running"
	b.mu.Unlock()
	b.log("load phase started: %ds sustained", req.Duration)

	var wg sync.WaitGroup
	for v := 0; v < req.Concurrency; v++ {
		wg.Add(1)
		go func(vu int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(vu)))
			for time.Now().Before(deadline) {
				select {
				case <-ctx.Done():
					return
				default:
				}
				code := codes[rng.Intn(len(codes))]
				rq, _ := http.NewRequest("GET", base+code, nil)
				rq.Header.Set("User-Agent", "k6/0.52 kortex-loadgen")
				t0 := time.Now()
				resp, err := client.Do(rq)
				us := time.Since(t0).Microseconds()
				if err != nil {
					b.errs.Add(1)
					continue
				}
				resp.Body.Close()
				if resp.StatusCode >= 400 {
					b.errs.Add(1)
				}
				b.hist.Observe(us)
				b.reqs.Add(1)
			}
		}(v)
	}

	tickDone := make(chan struct{})
	go func() {
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		last := int64(0)
		lastT := time.Now()
		for {
			select {
			case <-tickDone:
				return
			case now := <-t.C:
				cur := b.reqs.Load()
				dt := now.Sub(lastT).Seconds()
				rps := float64(cur-last) / dt
				el := time.Since(start).Seconds()
				p99 := b.hist.Percentile(0.99)
				b.mu.Lock()
				b.snap.Elapsed = el
				b.snap.Progress = el / float64(req.Duration)
				b.snap.RPS = rps
				if rps > b.snap.PeakRPS {
					b.snap.PeakRPS = rps
				}
				b.snap.P99 = p99
				b.snap.P50 = b.hist.Percentile(0.50)
				b.snap.Mean = b.hist.Mean()
				b.snap.Series = append(b.snap.Series, BenchTick{T: el, RPS: rps, P99: p99})
				b.mu.Unlock()
				last, lastT = cur, now
			}
		}
	}()

	wg.Wait()
	close(tickDone)
	elapsed := time.Since(start).Seconds()
	total := b.reqs.Load()
	h1, m1 := a.cache.Hits.Load(), a.cache.Misses.Load()
	hits, misses := h1-h0, m1-m0
	rate := 0.0
	if hits+misses > 0 {
		rate = float64(hits) / float64(hits+misses) * 100
	}

	b.log("load phase complete: %d requests in %.2fs", total, elapsed)
	b.log("draining event bus into columnar store ...")
	a.bus.Flush(8 * time.Second)

	b.mu.Lock()
	b.snap.Elapsed = elapsed
	b.snap.RPS = float64(total) / elapsed
	b.snap.Mean = b.hist.Mean()
	b.snap.P50 = b.hist.Percentile(0.50)
	b.snap.P90 = b.hist.Percentile(0.90)
	b.snap.P95 = b.hist.Percentile(0.95)
	b.snap.P99 = b.hist.Percentile(0.99)
	b.snap.Max = float64(b.hist.max.Load()) / 1000
	b.snap.CacheHitRate = rate
	b.snap.CacheHits = hits
	b.snap.CacheMiss = misses
	b.snap.EventsQueued = uint64(total)
	b.snap.EventsStored = a.olap.TotalRows.Load()
	b.mu.Unlock()

	b.log("throughput %.0f req/s | p50 %.2fms | p99 %.2fms | errors %d", float64(total)/elapsed, b.snap.P50, b.snap.P99, b.errs.Load())
	b.log("redis hit rate %.2f%% (%d hits / %d misses)", rate, hits, misses)
	b.log("bus lag drained, columnar rows now %d", a.olap.TotalRows.Load())
}

func (a *App) runIngestBench(ctx context.Context, n int, codes []string) {
	b := a.bench
	defer func() {
		b.running.Store(false)
		b.mu.Lock()
		b.snap.Running = false
		b.snap.Phase = "done"
		b.snap.Progress = 1
		b.mu.Unlock()
	}()

	b.log("synthesising %d click events across %d codes", n, len(codes))
	b.mu.Lock()
	b.snap.Phase = "running"
	b.mu.Unlock()

	rowsBefore := a.olap.TotalRows.Load()
	start := time.Now()
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	batch := make([]ClickEvent, 0, 20000)
	now := time.Now().UnixMilli()
	produced := 0
	for i := 0; i < n; i++ {
		select {
		case <-ctx.Done():
			b.log("cancelled at %d events", i)
			goto finish
		default:
		}
		country, city := pickCountry(rng)
		dev, brw, os := pickDevice(rng)
		batch = append(batch, ClickEvent{
			TS: now - int64(rng.Intn(3600000)), Code: codes[rng.Intn(len(codes))],
			Country: country, City: city, Ref: pickReferrer(rng), Device: dev,
			Browser: brw, OS: os, Bot: rng.Intn(100) < 4, Status: 302, Hit: true,
		})
		produced++
		if len(batch) == cap(batch) {
			a.olap.Insert(batch)
			batch = batch[:0]
			el := time.Since(start).Seconds()
			b.mu.Lock()
			b.snap.Progress = float64(produced) / float64(n)
			b.snap.Elapsed = el
			b.snap.EventsQueued = uint64(produced)
			b.snap.EventsStored = a.olap.TotalRows.Load()
			b.snap.IngestRate = float64(produced) / el
			b.mu.Unlock()
			if produced%200000 == 0 {
				b.log("%d events materialised | %.0f evt/s", produced, float64(produced)/el)
			}
		}
	}
	if len(batch) > 0 {
		a.olap.Insert(batch)
	}
finish:
	el := time.Since(start).Seconds()
	rows := a.olap.TotalRows.Load()
	b.mu.Lock()
	b.snap.Elapsed = el
	b.snap.EventsQueued = uint64(produced)
	b.snap.EventsStored = rows
	b.snap.IngestRate = float64(produced) / el
	b.mu.Unlock()
	b.log("ingested %d events in %.2fs = %.0f events/sec", produced, el, float64(produced)/el)
	b.log("table rows %d -> %d across %d day partitions", rowsBefore, rows, len(a.olap.Stats().Partitions))
	b.log("materialized views updated inline; no re-scan required")
}

func sortedFloats(v []float64) []float64 {
	sort.Float64s(v)
	return v
}
