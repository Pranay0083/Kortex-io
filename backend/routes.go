package main

// HTTP routing and middleware.

import (
	"net/http"
	"os"
	"path"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func (a *App) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer) // a panic in one handler becomes a 500, not a crash
	r.Use(corsMiddleware(a.cfg.CORSOrigins))

	r.Route("/api", func(r chi.Router) {
		r.Use(limitBody)
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) { writeErr(w, http.StatusNotFound, "not found") })

		r.Get("/", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{"service": "kortex", "status": "ok"})
		})
		r.Get("/health", a.handleHealth)

		// auth
		r.Post("/auth/register", a.handleRegister)
		r.Post("/auth/login", a.handleLogin)
		r.Post("/auth/demo", a.handleDemoLogin)
		r.Post("/auth/refresh", a.handleRefresh)
		r.Post("/auth/logout", a.handleLogout)
		r.With(a.requireAuth).Get("/auth/me", a.handleMe)

		// read path (public)
		r.Get("/r/{code}", a.handleRedirect)
		r.Head("/r/{code}", a.handleRedirect)
		r.Get("/resolve/{code}", a.handleResolveJSON)
		r.Get("/qr/{code}", a.handleQR)

		// write path, analytics and benchmarks (logged in)
		r.Group(func(r chi.Router) {
			r.Use(a.requireAuth)
			r.Post("/links", a.handleShorten)
			r.Get("/links", a.handleListLinks)
			r.Get("/links/check-alias", a.handleCheckAlias)
			r.Get("/links/{code}", a.handleGetLink)
			r.Delete("/links/{code}", a.handleDeleteLink)
			r.Post("/links/{code}/toggle", a.handleToggleLink)

			r.Get("/analytics/overview", a.handleOverview)
			r.Get("/analytics/{code}/timeseries", a.handleTimeseries)
			r.Get("/analytics/{code}/breakdown", a.handleBreakdown)
			r.Get("/analytics/{code}/summary", a.handleSummary)
			r.Get("/analytics/{code}/race", a.handleQueryRace)

			r.Post("/bench/start", a.handleBenchStart)
			r.Post("/bench/stop", a.handleBenchStop)
			r.Get("/stream/recent", a.handleStream)
		})

		// ops + lab (public)
		r.Get("/bench/status", a.handleBenchStatus)
		r.Get("/system/stats", a.handleSystemStats)

		r.Get("/lab/snowflake", a.handleDecodeID)
		r.Get("/lab/snowflake/burst", a.handleIDBurst)
		r.Get("/lab/ring", a.handleRing)
		r.Get("/lab/bloom", a.handleBloomState)
		r.Post("/lab/bloom/reset", a.handleBloomReset)
		r.Post("/lab/bloom/add", a.handleBloomAdd)
		r.Get("/lab/bloom/test", a.handleBloomTest)
		r.Get("/lab/bloom/sweep", a.handleBloomSweep)
	})

	// Short links at the root: kortex.link/{code}.
	r.Get("/{code}", a.handleRoot)
	r.Head("/{code}", a.handleRoot)

	if a.static != "" {
		r.NotFound(spaHandler(a.static))
	}
	return r
}

// corsMiddleware only echoes back allow-listed origins, so arbitrary sites
// never get credentialed access.
func corsMiddleware(allowed map[string]bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Add("Vary", "Origin")
			if origin := r.Header.Get("Origin"); origin != "" && allowed[normOrigin(origin)] {
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Credentials", "true")
				h.Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Authorization,Content-Type,X-Requested-With")
				h.Set("Access-Control-Expose-Headers", "X-Kortex-Cache,X-RateLimit-Remaining")
				h.Set("Access-Control-Max-Age", "600")
			}
			// preflight request: answer it here
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// spaHandler serves the frontend build. Unknown paths get index.html so
// client-side routes work. A missing path that looks like a file (has an
// extension) gets a 404, so a stale JS chunk is never answered with HTML.
func spaHandler(dir string) http.HandlerFunc {
	fileServer := http.FileServer(http.Dir(dir))
	index := filepath.Join(dir, "index.html")
	return func(w http.ResponseWriter, r *http.Request) {
		p := path.Clean("/" + r.URL.Path)
		if fi, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err == nil && fi.Mode().IsRegular() {
			fileServer.ServeHTTP(w, r)
			return
		}
		if path.Ext(p) != "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, index)
	}
}
