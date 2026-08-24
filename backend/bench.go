package main

// Built-in benchmarks (admin only), run from the ops console:
//   - redirect: N goroutines ("virtual users") hit /api/r/{code} over
//     loopback HTTP for D seconds, recording a latency histogram.
//   - ingest: generate M synthetic click events and insert them straight
//     into the OLAP store, measuring events/sec.
//
// Both only touch the admin-owned "bench-NN" links, so load tests never
// show up in real or demo analytics. Only one benchmark runs at a time.

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
)

// --- latency histogram ---------------------------------------------------

// Fixed-width buckets of 10us up to 400ms; slower requests land in the last
// bucket. Every field is atomic, so many goroutines can record at once
// without a lock.
const (
	histBucketUS = 10
	histBuckets  = 40000
)

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
	h.buckets[min(us/histBucketUS, histBuckets)].Add(1)
	h.count.Add(1)
	h.sum.Add(us)
	// lock-free max: retry the compare-and-swap until we win or aren't bigger
	for {
		cur := h.max.Load()
		if us <= cur || h.max.CompareAndSwap(cur, us) {
			break
		}
	}
}

// Percentile returns the p-th percentile (0..1) in milliseconds: walk the
// buckets until we've passed p of all samples.
func (h *Histogram) Percentile(p float64) float64 {
	total := h.count.Load()
	if total == 0 {
		return 0
	}
	target := int64(float64(total) * p)
	var seen int64
	for i := range h.buckets {
		seen += h.buckets[i].Load()
		if seen >= target {
			return float64(i*histBucketUS) / 1000
		}
	}
	return h.MaxMS()
}

func (h *Histogram) Mean() float64 {
	c := h.count.Load()
	if c == 0 {
		return 0
	}
	return float64(h.sum.Load()) / float64(c) / 1000
}

func (h *Histogram) MaxMS() float64 { return float64(h.max.Load()) / 1000 }

// --- benchmark state -----------------------------------------------------

type BenchSnapshot struct {
	ID           string      `json:"id"`
	Kind         string      `json:"kind"`
	Running      bool        `json:"running"`
	Phase        string      `json:"phase"`
	StartedAt    int64       `json:"started_at"`
	DurationS    int         `json:"duration_s"`
	Concurrency  int         `json:"concurrency"`
	Elapsed      float64     `json:"elapsed_s"`
	Progress     float64     `json:"progress"`
	Requests     int64       `json:"requests"`
	Errors       int64       `json:"errors"`
	RPS          float64     `json:"rps"`
	PeakRPS      float64     `json:"peak_rps"`
	Mean         float64     `json:"mean_ms"`
	P50          float64     `json:"p50_ms"`
	P90          float64     `json:"p90_ms"`
	P95          float64     `json:"p95_ms"`
	P99          float64     `json:"p99_ms"`
	Max          float64     `json:"max_ms"`
	CacheHitRate float64     `json:"cache_hit_rate"`
	CacheHits    uint64      `json:"cache_hits"`
	CacheMiss    uint64      `json:"cache_misses"`
	EventsQueued uint64      `json:"events_queued"`
	EventsStored uint64      `json:"events_stored"`
	IngestRate   float64     `json:"ingest_rate"`
	Log          []string    `json:"log"`
	Series       []BenchTick `json:"series"`
	// The last finished run of each kind, so a load test doesn't hide the
	// previous ingest result (and vice versa). Log and series are omitted.
	LastRedirect *BenchSnapshot `json:"last_redirect,omitempty"`
	LastIngest   *BenchSnapshot `json:"last_ingest,omitempty"`
}

type BenchTick struct {
	T   float64 `json:"t"`
	RPS float64 `json:"rps"`
	P99 float64 `json:"p99"`
}

type Bench struct {
	mu     sync.Mutex
	snap   BenchSnapshot
	last   map[string]*BenchSnapshot // kind -> last finished run
	hist   *Histogram
	cancel context.CancelFunc

	running atomic.Bool
	reqs    atomic.Int64
	errs    atomic.Int64
}

const benchLogLines = 120

func NewBench() *Bench {
	return &Bench{hist: NewHistogram(), snap: BenchSnapshot{Phase: "idle"}, last: map[string]*BenchSnapshot{}}
}

// update changes the snapshot under the lock.
func (b *Bench) update(f func(s *BenchSnapshot)) {
	b.mu.Lock()
	f(&b.snap)
	b.mu.Unlock()
}

func (b *Bench) setPhase(phase string) { b.update(func(s *BenchSnapshot) { s.Phase = phase }) }

// Stop cancels the running benchmark, if any.
func (b *Bench) Stop() {
	b.mu.Lock()
	if b.cancel != nil {
		b.cancel()
	}
	b.mu.Unlock()
}

// finish marks the current run done and keeps it as the last result of its kind.
func (b *Bench) finish() {
	b.running.Store(false)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.snap.Running = false
	b.snap.Phase = "done"
	b.snap.Progress = 1
	done := b.snap
	done.Requests = b.reqs.Load()
	done.Errors = b.errs.Load()
	done.Log, done.Series, done.LastRedirect, done.LastIngest = nil, nil, nil, nil
	b.last[done.Kind] = &done
}

func (b *Bench) Snapshot() BenchSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.snap
	s.Requests = b.reqs.Load()
	s.Errors = b.errs.Load()
	s.LastRedirect, s.LastIngest = b.last["redirect"], b.last["ingest"]
	return s
}

// log appends a timestamped line to the run log shown in the UI.
func (b *Bench) log(format string, args ...any) {
	line := fmt.Sprintf("%-8s %s", time.Now().UTC().Format("15:04:05"), fmt.Sprintf(format, args...))
	b.update(func(s *BenchSnapshot) {
		s.Log = append(s.Log, line)
		if len(s.Log) > benchLogLines {
			s.Log = s.Log[len(s.Log)-benchLogLines:]
		}
	})
}

// --- HTTP handlers -------------------------------------------------------

type benchReq struct {
	Duration    int    `json:"duration_s"`
	Concurrency int    `json:"concurrency"`
	Kind        string `json:"kind"`
	Events      int    `json:"events"`
}

// normalize fills defaults (0 means "use the default") and clamps the rest
// to a safe range.
func (req *benchReq) normalize() {
	orDefault := func(v, def int) int {
		if v == 0 {
			return def
		}
		return v
	}
	req.Duration = clamp(orDefault(req.Duration, 10), 5, 60)
	req.Concurrency = clamp(orDefault(req.Concurrency, 64), 1, 512)
	req.Events = clamp(orDefault(req.Events, 1000000), 1000, 5000000)
	if req.Kind != "ingest" {
		req.Kind = "redirect"
	}
}

func (a *App) handleBenchStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.bench.Snapshot())
}

func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if u := currentUser(r); u == nil || u.Role != "admin" {
		writeErr(w, http.StatusForbidden, "benchmarks are restricted to the admin account")
		return false
	}
	return true
}

func (a *App) handleBenchStop(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	a.bench.Stop()
	writeJSON(w, http.StatusOK, map[string]bool{"stopping": true})
}

func (a *App) handleBenchStart(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	var req benchReq
	_ = readJSON(r, &req)
	req.normalize()

	// CompareAndSwap makes "check nothing is running, then mark running"
	// one atomic step, so two clicks can't start two benchmarks.
	if !a.bench.running.CompareAndSwap(false, true) {
		writeErr(w, http.StatusConflict, "a benchmark is already running")
		return
	}
	codes, err := a.benchCodes(r.Context())
	if err != nil || len(codes) == 0 {
		a.bench.running.Store(false)
		if err != nil {
			a.internalErr(w, "bench codes", err)
			return
		}
		writeErr(w, http.StatusBadRequest, "the benchmark links are missing or disabled — restart the server to re-create them")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	b := a.bench
	b.mu.Lock()
	b.hist = NewHistogram()
	b.reqs.Store(0)
	b.errs.Store(0)
	b.cancel = cancel
	b.snap = BenchSnapshot{
		ID: strconv.FormatInt(time.Now().UnixNano(), 36), Kind: req.Kind, Running: true,
		Phase: "warming", StartedAt: time.Now().UnixMilli(), DurationS: req.Duration,
		Concurrency: req.Concurrency, Log: []string{}, Series: []BenchTick{},
	}
	b.mu.Unlock()

	if req.Kind == "ingest" {
		go a.runIngestBench(ctx, req.Events, codes)
	} else {
		go a.runRedirectBench(ctx, req, codes)
	}
	writeJSON(w, http.StatusAccepted, b.Snapshot())
}

// benchCodes returns the admin-owned benchmark links (see seedBenchLinks)
// that are still live.
func (a *App) benchCodes(ctx context.Context) ([]string, error) {
	out := []string{}
	for _, code := range a.benchLinks {
		l, err := a.store.GetByCode(ctx, code)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if l.UserID == a.adminID && l.Active && l.ExpiresAt == nil {
			out = append(out, l.Code)
		}
	}
	return out, nil
}

// --- redirect benchmark --------------------------------------------------

// loadClient is an HTTP client tuned for load generation: enough keep-alive
// connections for every worker, and it never follows redirects (we measure
// our 302, not the destination site).
func loadClient(concurrency int) *http.Client {
	conns := concurrency * 2
	return &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        conns,
			MaxIdleConnsPerHost: conns,
			MaxConnsPerHost:     conns,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  true,
			DialContext:         (&net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		},
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// hit sends one GET and returns its latency, or an error.
func hit(client *http.Client, url, ua string) (time.Duration, int, error) {
	rq, _ := http.NewRequest(http.MethodGet, url, nil)
	rq.Header.Set("User-Agent", ua)
	t0 := time.Now()
	resp, err := client.Do(rq)
	if err != nil {
		return 0, 0, err
	}
	resp.Body.Close()
	return time.Since(t0), resp.StatusCode, nil
}

func (a *App) runRedirectBench(ctx context.Context, req benchReq, codes []string) {
	b := a.bench
	defer b.finish()

	base := "http://127.0.0.1:" + a.cfg.Port + "/api/r/"
	client := loadClient(req.Concurrency)

	b.log("k6-style loopback runner: %d VUs -> %s{code}", req.Concurrency, base)
	b.log("target pool: %d active short codes across %d shards", len(codes), a.cfg.ShardCount)
	b.log("warming connection pool + redis working set ...")
	for i := 0; i < len(codes) && i < 64; i++ {
		_, _, _ = hit(client, base+codes[i], "k6/kortex-warmup")
	}

	hits0, misses0 := a.cache.Hits.Load(), a.cache.Misses.Load()
	start := time.Now()
	deadline := start.Add(time.Duration(req.Duration) * time.Second)
	b.setPhase("running")
	b.log("load phase started: %ds sustained", req.Duration)

	// Each worker sends requests one after another until the deadline.
	var wg sync.WaitGroup
	for vu := 0; vu < req.Concurrency; vu++ {
		wg.Add(1)
		go func(vu int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(vu)))
			for time.Now().Before(deadline) && ctx.Err() == nil {
				latency, status, err := hit(client, base+codes[rng.Intn(len(codes))], "k6/0.52 kortex-loadgen")
				if err != nil {
					b.errs.Add(1)
					continue
				}
				if status >= 400 {
					b.errs.Add(1)
				}
				b.hist.Observe(latency.Microseconds())
				b.reqs.Add(1)
			}
		}(vu)
	}
	stopSampler := b.sampleProgress(start, req.Duration)
	wg.Wait()
	stopSampler()

	elapsed := time.Since(start).Seconds()
	total := b.reqs.Load()
	hits, misses := a.cache.Hits.Load()-hits0, a.cache.Misses.Load()-misses0
	rate := hitRate(hits, misses)

	b.log("load phase complete: %d requests in %.2fs", total, elapsed)
	b.log("draining event bus into columnar store ...")
	a.bus.Flush(8 * time.Second)

	h := b.hist
	b.update(func(s *BenchSnapshot) {
		s.Elapsed = elapsed
		s.RPS = float64(total) / elapsed
		s.Mean = h.Mean()
		s.P50, s.P90, s.P95, s.P99 = h.Percentile(0.50), h.Percentile(0.90), h.Percentile(0.95), h.Percentile(0.99)
		s.Max = h.MaxMS()
		s.CacheHitRate, s.CacheHits, s.CacheMiss = rate, hits, misses
		s.EventsQueued = uint64(total)
		s.EventsStored = a.olap.TotalRows.Load()
	})
	b.log("throughput %.0f req/s | p50 %.2fms | p99 %.2fms | errors %d",
		float64(total)/elapsed, h.Percentile(0.50), h.Percentile(0.99), b.errs.Load())
	b.log("redis hit rate %.2f%% (%d hits / %d misses)", rate, hits, misses)
	b.log("bus lag drained, columnar rows now %d", a.olap.TotalRows.Load())
}

// sampleProgress records RPS and latency every 500ms for the live chart.
// Call the returned function to stop it.
func (b *Bench) sampleProgress(start time.Time, durationS int) (stop func()) {
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		lastReqs, lastT := int64(0), time.Now()
		for {
			select {
			case <-done:
				return
			case now := <-t.C:
				reqs := b.reqs.Load()
				rps := float64(reqs-lastReqs) / now.Sub(lastT).Seconds()
				elapsed := time.Since(start).Seconds()
				p99 := b.hist.Percentile(0.99)
				p50, mean := b.hist.Percentile(0.50), b.hist.Mean()
				b.update(func(s *BenchSnapshot) {
					s.Elapsed = elapsed
					s.Progress = elapsed / float64(durationS)
					s.RPS = rps
					s.PeakRPS = max(s.PeakRPS, rps)
					s.P99, s.P50, s.Mean = p99, p50, mean
					s.Series = append(s.Series, BenchTick{T: elapsed, RPS: rps, P99: p99})
				})
				lastReqs, lastT = reqs, now
			}
		}
	}()
	return func() { close(done) }
}

// --- ingest benchmark ----------------------------------------------------

const ingestBatch = 20000

func (a *App) runIngestBench(ctx context.Context, n int, codes []string) {
	b := a.bench
	defer b.finish()

	b.log("synthesising %d click events across %d codes", n, len(codes))
	b.setPhase("running")

	rowsBefore := a.olap.TotalRows.Load()
	start := time.Now()
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	now := time.Now().UnixMilli()
	batch := make([]ClickEvent, 0, ingestBatch)
	produced := 0

	report := func() {
		el := time.Since(start).Seconds()
		b.update(func(s *BenchSnapshot) {
			s.Elapsed = el
			s.EventsQueued = uint64(produced)
			s.EventsStored = a.olap.TotalRows.Load()
			s.IngestRate = float64(produced) / el
		})
	}

	for produced < n {
		if ctx.Err() != nil {
			b.log("cancelled at %d events", produced)
			batch = batch[:0] // a cancelled run doesn't write its partial batch
			break
		}
		country, city := pickCountry(rng)
		dev, brw, os := pickDevice(rng)
		batch = append(batch, ClickEvent{
			TS: now - int64(rng.Intn(int(hourMS))), Code: codes[rng.Intn(len(codes))], UserID: a.adminID,
			Country: country, City: city, Ref: pickReferrer(rng), Device: dev,
			Browser: brw, OS: os, Bot: rng.Intn(100) < 4, Status: http.StatusFound, Hit: true,
		})
		produced++
		if len(batch) == cap(batch) {
			a.olap.Insert(batch)
			batch = batch[:0]
			b.update(func(s *BenchSnapshot) { s.Progress = float64(produced) / float64(n) })
			report()
			if produced%200000 == 0 {
				b.log("%d events materialised | %.0f evt/s", produced, float64(produced)/time.Since(start).Seconds())
			}
		}
	}
	a.olap.Insert(batch)
	report()

	el := time.Since(start).Seconds()
	b.log("ingested %d events in %.2fs = %.0f events/sec", produced, el, float64(produced)/el)
	b.log("table rows %d -> %d across %d day partitions", rowsBefore, a.olap.TotalRows.Load(), len(a.olap.Stats().Partitions))
	b.log("materialized views updated inline; no re-scan required")
}
