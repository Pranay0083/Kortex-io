package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
)

type App struct {
	cfg       Config
	sf        *Snowflake
	ring      *Ring
	store     *Store
	cache     *Cache
	bus       *Broker
	olap      *OLAP
	bloom     *Bloom
	labBloom  *Bloom
	labKeys   []string
	bench     *Bench
	started   time.Time
	redirects atomic.Uint64
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type,X-Requested-With")
		w.Header().Set("Access-Control-Expose-Headers", "X-Kortex-Cache,X-RateLimit-Remaining")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) countRedirects(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.redirects.Add(1)
		next.ServeHTTP(w, r)
	})
}

// Dependencies are supervised siblings, so tolerate them coming up late.
func waitForStore(ctx context.Context, url string, ring *Ring) (*Store, error) {
	var last error
	for i := 0; i < 60; i++ {
		s, err := NewStore(ctx, url, ring)
		if err == nil {
			return s, nil
		}
		last = err
		if i == 0 {
			log.Printf("waiting for postgres: %v", err)
		}
		time.Sleep(2 * time.Second)
	}
	return nil, last
}

func waitForCache(url string, ttl int) (*Cache, error) {
	var last error
	for i := 0; i < 30; i++ {
		c, err := NewCache(url, ttl)
		if err == nil {
			return c, nil
		}
		last = err
		time.Sleep(2 * time.Second)
	}
	return nil, last
}

func main() {
	_ = godotenv.Load("/app/backend/.env")
	cfg := loadConfig()
	if cfg.JWTSecret == "" {
		log.Fatal("JWT_SECRET is required")
	}
	ctx := context.Background()

	app := &App{
		cfg:      cfg,
		sf:       NewSnowflake(cfg.NodeID),
		ring:     NewRing(cfg.ShardCount, cfg.VNodes),
		olap:     NewOLAP(),
		bloom:    NewBloom(1000000, 0.001),
		labBloom: NewBloom(1000, 0.01),
		labKeys:  []string{},
		bench:    NewBench(),
		started:  time.Now(),
	}

	store, err := waitForStore(ctx, cfg.PGURL, app.ring)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	app.store = store

	cache, err := waitForCache(cfg.RedisURL, cfg.CacheTTL)
	if err != nil {
		log.Fatalf("redis: %v", err)
	}
	app.cache = cache

	app.bus = NewBroker("clicks", cfg.Partitions, 200000, app.olap.Insert)
	app.bus.Start()
	app.olap.StartMerger()
	store.StartClickFlusher(ctx)

	if err := app.seedDemo(ctx); err != nil {
		log.Printf("seed warning: %v", err)
	}

	// cache warming: hottest links first
	if links, err := store.AllCodes(ctx); err == nil {
		n := cache.Warm(ctx, links, 5000)
		log.Printf("cache warmed with %d links", n)
		for _, l := range links {
			app.bloom.Add(l.Code)
			app.olap.CodeID(l.Code)
		}
	}

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(cors)

	r.Route("/api", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, rq *http.Request) {
			writeJSON(w, 200, map[string]string{"service": "kortex", "status": "ok"})
		})
		r.Get("/health", app.handleHealth)

		r.Post("/auth/register", app.handleRegister)
		r.Post("/auth/login", app.handleLogin)
		r.Post("/auth/refresh", app.handleRefresh)
		r.Group(func(r chi.Router) {
			r.Use(app.requireAuth)
			r.Get("/auth/me", app.handleMe)
			r.Post("/auth/logout", app.handleLogout)
		})

		// read path
		r.With(app.countRedirects).Get("/r/{code}", app.handleRedirect)
		r.Get("/resolve/{code}", app.handleResolveJSON)
		r.Get("/qr/{code}", app.handleQR)

		// write path + management
		r.Group(func(r chi.Router) {
			r.Use(app.requireAuth)
			r.Post("/links", app.handleShorten)
			r.Get("/links", app.handleListLinks)
			r.Get("/links/check-alias", app.handleCheckAlias)
			r.Get("/links/{code}", app.handleGetLink)
			r.Delete("/links/{code}", app.handleDeleteLink)
			r.Post("/links/{code}/toggle", app.handleToggleLink)

			r.Get("/analytics/overview", app.handleOverview)
			r.Get("/analytics/{code}/timeseries", app.handleTimeseries)
			r.Get("/analytics/{code}/breakdown", app.handleBreakdown)
			r.Get("/analytics/{code}/summary", app.handleSummary)
			r.Get("/analytics/{code}/race", app.handleQueryRace)

			r.Post("/bench/start", app.handleBenchStart)
			r.Post("/bench/stop", app.handleBenchStop)
		})

		r.Get("/bench/status", app.handleBenchStatus)
		r.Get("/stream/recent", app.handleStream)
		r.Get("/system/stats", app.handleSystemStats)

		r.Get("/lab/snowflake", app.handleDecodeID)
		r.Get("/lab/snowflake/burst", app.handleIDBurst)
		r.Get("/lab/ring", app.handleRing)
		r.Get("/lab/bloom", app.handleBloomState)
		r.Post("/lab/bloom/reset", app.handleBloomReset)
		r.Post("/lab/bloom/add", app.handleBloomAdd)
		r.Get("/lab/bloom/test", app.handleBloomTest)
		r.Get("/lab/bloom/sweep", app.handleBloomSweep)
	})

	// In production nginx maps kortex.link/{code} straight here.
	r.With(app.countRedirects).Get("/{code}", app.handleRedirect)

	addr := "0.0.0.0:" + cfg.Port
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  90 * time.Second,
	}
	log.Printf("kortex listening on %s | shards=%d partitions=%d node=%d rows=%d",
		addr, cfg.ShardCount, cfg.Partitions, cfg.NodeID, app.olap.TotalRows.Load())
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
		os.Exit(1)
	}
}
