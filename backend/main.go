package main

// Entry point: load config, connect to Postgres and Redis, start the
// background workers, serve HTTP, and shut down cleanly on SIGINT/SIGTERM.

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/oschwald/geoip2-golang"
)

// App holds every component. Handlers are methods on *App, so they reach
// what they need through `a.` instead of globals.
type App struct {
	cfg   Config
	ids   *Snowflake // ID generator
	ring  *Ring      // code -> shard
	store *Store     // Postgres
	cache *Cache     // Redis
	bus   *Broker    // click events -> OLAP
	olap  *OLAP      // analytics
	bloom *Bloom     // "does this code exist?" filter
	bench *Bench
	geo   *geoip2.Reader // nil unless GEOIP_DB is set

	labMu    sync.Mutex // guards labBloom and labKeys
	labBloom *Bloom
	labKeys  []string

	static     string          // frontend build dir, "" when not serving it
	demoCodes  map[string]bool // written once at boot, read-only afterwards
	demoUserID int64           // shared trial account; 0 when DEMO_MODE is off
	adminID    int64           // owner of the benchmark links
	benchLinks []string        // codes the load generator targets, set at boot

	started      time.Time
	redirects    atomic.Uint64
	unknownCodes atomic.Uint64
}

const (
	bloomCapacity   = 1000000 // codes the main filter is sized for
	bloomFPRate     = 0.001
	busRetention    = 200000 // events kept per partition
	cacheWarmLinks  = 5000
	shutdownTimeout = 15 * time.Second
	busDrainTimeout = 3 * time.Second
	retryDelay      = 2 * time.Second // wait between connection attempts
)

func newApp(cfg Config) *App {
	a := &App{
		cfg:      cfg,
		ids:      NewSnowflake(cfg.NodeID),
		ring:     NewRing(cfg.ShardCount, cfg.VNodes),
		olap:     NewOLAP(cfg.RetentionDays),
		bloom:    NewBloom(bloomCapacity, bloomFPRate),
		labBloom: NewBloom(1000, 0.01),
		labKeys:  []string{},
		bench:    NewBench(),
		started:  time.Now(),
	}
	if fi, err := os.Stat(filepath.Join(cfg.StaticDir, "index.html")); err == nil && fi.Mode().IsRegular() {
		a.static = cfg.StaticDir
	}
	return a
}

// retry calls connect up to `attempts` times. In Docker Compose, Postgres and
// Redis may still be starting when we boot.
func retry[T any](name string, attempts int, connect func() (T, error)) (T, error) {
	var last error
	for i := 0; i < attempts; i++ {
		v, err := connect()
		if err == nil {
			return v, nil
		}
		last = err
		if i == 0 {
			log.Printf("waiting for %s: %v", name, err)
		}
		time.Sleep(retryDelay)
	}
	var zero T
	return zero, last
}

func main() {
	for _, f := range []string{".env", "../.env"} {
		_ = godotenv.Load(f)
	}
	cfg := loadConfig()
	if err := validateJWTSecret(cfg.JWTSecret); err != nil {
		log.Fatalf("refusing to start: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := newApp(cfg)
	if closeGeo := app.openGeoIP(); closeGeo != nil {
		defer closeGeo()
	}

	// 1. dependencies
	var err error
	app.store, err = retry("postgres", 60, func() (*Store, error) { return NewStore(ctx, cfg.PGURL, app.ring) })
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	app.cache, err = retry("redis", 30, func() (*Cache, error) { return NewCache(cfg.RedisURL, cfg.CacheTTL) })
	if err != nil {
		log.Fatalf("redis: %v", err)
	}

	// 2. background workers
	app.bus = NewBroker("clicks", cfg.Partitions, busRetention, app.olap.Insert)
	app.bus.Start()
	app.olap.StartMerger()
	flushCtx, stopFlusher := context.WithCancel(context.Background())
	app.store.StartClickFlusher(flushCtx)

	// 3. data: seed, then warm Redis and the Bloom filter
	if err := app.seedDemo(ctx); err != nil {
		log.Printf("seed warning: %v", err)
	}
	app.warmUp(ctx)

	// 4. serve until a signal arrives
	srv := &http.Server{
		Addr:              "0.0.0.0:" + cfg.Port,
		Handler:           app.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	if app.static != "" {
		log.Printf("serving frontend from %s", app.static)
	}
	log.Printf("kortex listening on %s | node=%s shards=%d partitions=%d rows=%d",
		srv.Addr, cfg.NodeName, cfg.ShardCount, cfg.Partitions, app.olap.TotalRows.Load())
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()
	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	case <-ctx.Done():
	}

	// 5. graceful shutdown
	app.shutdown(srv, stopFlusher)
}

// openGeoIP loads the GeoLite2 database if configured. It returns a close
// function, or nil if geo lookup is off.
func (a *App) openGeoIP() func() {
	if a.cfg.GeoIPDB == "" {
		return nil
	}
	db, err := geoip2.Open(a.cfg.GeoIPDB)
	if err != nil {
		log.Printf("geoip disabled: %v", err)
		return nil
	}
	a.geo = db
	log.Printf("geoip enabled from %s", a.cfg.GeoIPDB)
	return func() { db.Close() }
}

// warmUp loads the busiest links into Redis and every code into the Bloom
// filter, so the first requests after a deploy don't all go to Postgres.
func (a *App) warmUp(ctx context.Context) {
	links, err := a.store.AllCodes(ctx)
	if err != nil {
		log.Printf("warm-up skipped: %v", err)
		return
	}
	log.Printf("cache warmed with %d links", a.cache.Warm(ctx, links, cacheWarmLinks))
	for _, l := range links {
		a.bloom.Add(l.Code)
	}
}

// shutdown stops in dependency order: stop taking requests, drain in-flight
// clicks into analytics, write pending click counters, close the pool.
func (a *App) shutdown(srv *http.Server, stopFlusher context.CancelFunc) {
	log.Printf("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("http shutdown: %v", err)
	}
	a.bench.Stop()
	a.bus.Flush(busDrainTimeout)
	a.bus.Stop()
	stopFlusher()
	a.store.flushClicks(ctx)
	a.store.pool.Close()
	log.Printf("bye")
}
