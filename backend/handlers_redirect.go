package main

// The read path: turning a short code into a redirect. This is the hot path.
//
//	GET /{code}
//	  -> Redis hit?            yes: redirect (no DB at all)
//	  -> Bloom filter says no? yes: 404 (no DB at all)
//	  -> SELECT from the one shard that owns the code, then cache it
//	  -> 30x redirect
//	  -> async: bump click counter (batched to Postgres) + emit ClickEvent
//	     (non-blocking send to the event bus -> OLAP)

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

var errUnknownCode = errors.New("unknown short code")

// resolve finds a link: Redis, then the Bloom filter, then the owning
// shard. It reports whether Redis had it. errUnknownCode means the code
// doesn't exist; any other error means a dependency is down.
func (a *App) resolve(ctx context.Context, code string) (cl *CachedLink, cacheHit bool, err error) {
	if !codeShape(code) {
		return nil, false, errUnknownCode
	}
	if cl, ok := a.cache.Get(ctx, code); ok {
		return cl, true, nil
	}
	if a.bloomRulesOut(code) {
		return nil, false, errUnknownCode
	}
	l, err := a.store.GetByCode(ctx, code)
	if errors.Is(err, pgx.ErrNoRows) {
		a.bloom.FalsePos.Add(1)
		return nil, false, errUnknownCode
	}
	if err != nil {
		return nil, false, err
	}
	a.cache.Set(ctx, l) // cache-aside: fill Redis on a miss
	return toCached(l), false, nil
}

// handleRedirect serves /api/r/{code}.
func (a *App) handleRedirect(w http.ResponseWriter, r *http.Request) {
	a.redirect(w, r, chi.URLParam(r, "code"), false)
}

// handleRoot serves /{code}. Real static files and frontend routes win;
// anything else is treated as a short code. A browser asking for an unknown
// code gets the frontend's 404 page instead of JSON.
func (a *App) handleRoot(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	if a.static != "" {
		if spaRoutes[code] {
			a.serveIndex(w, r, http.StatusOK)
			return
		}
		p := filepath.Join(a.static, code)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			http.ServeFile(w, r, p)
			return
		}
	}
	a.redirect(w, r, code, true)
}

func acceptsHTML(r *http.Request) bool { return strings.Contains(r.Header.Get("Accept"), "text/html") }

func (a *App) serveIndex(w http.ResponseWriter, r *http.Request, status int) {
	b, err := os.ReadFile(filepath.Join(a.static, "index.html"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(b)
	}
}

// redirect answers GET and HEAD the same way, but only GET counts as a
// click (HEAD comes from link previews and uptime checks).
func (a *App) redirect(w http.ResponseWriter, r *http.Request, code string, fromRoot bool) {
	countClick := r.Method != http.MethodHead
	cl, hit, err := a.resolve(r.Context(), code)
	if errors.Is(err, errUnknownCode) {
		a.unknownCodes.Add(1)
		if fromRoot && a.static != "" && acceptsHTML(r) {
			a.serveIndex(w, r, http.StatusNotFound)
			return
		}
		writeErr(w, http.StatusNotFound, "unknown short code")
		return
	}
	if err != nil {
		log.Printf("resolve %q: %v", code, err)
		writeErr(w, http.StatusServiceUnavailable, "link store unavailable")
		return
	}

	if !cl.Active || cl.Expired(time.Now()) {
		if countClick {
			a.emitClick(r, code, cl.UserID, http.StatusGone, hit)
		}
		msg := "link expired"
		if !cl.Active {
			msg = "link disabled"
		}
		writeErr(w, http.StatusGone, msg)
		return
	}

	if countClick {
		a.store.BumpClicks(code, 1)
		a.emitClick(r, code, cl.UserID, cl.Redirect, hit)
	}
	a.redirects.Add(1)

	w.Header().Set("Location", cl.LongURL)
	w.Header().Set("Cache-Control", "private, max-age=0") // browsers must ask again, so every click is counted
	w.Header().Set("X-Kortex-Cache", cacheHeader(hit))
	w.WriteHeader(redirectStatus(cl.Redirect))
}

func cacheHeader(hit bool) string {
	if hit {
		return "HIT"
	}
	return "MISS"
}

// redirectStatus only lets through real redirect codes; anything else is 302.
func redirectStatus(s int) int {
	switch s {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return s
	}
	return http.StatusFound
}

// handleResolveJSON reports a link's status without redirecting. It never
// reveals where a disabled or expired link points.
func (a *App) handleResolveJSON(w http.ResponseWriter, r *http.Request) {
	cl, hit, err := a.resolve(r.Context(), chi.URLParam(r, "code"))
	if errors.Is(err, errUnknownCode) {
		writeErr(w, http.StatusNotFound, "unknown short code")
		return
	}
	if err != nil {
		log.Printf("resolve: %v", err)
		writeErr(w, http.StatusServiceUnavailable, "link store unavailable")
		return
	}
	expired := cl.Expired(time.Now())
	out := map[string]any{"cache": map[string]bool{"hit": hit}, "active": cl.Active, "expired": expired}
	if cl.ExpiresAt > 0 {
		out["expires_at"] = cl.ExpiresAt
	}
	switch {
	case !cl.Active:
		out["status"] = "disabled"
	case expired:
		out["status"] = "expired"
	default:
		out["status"] = "active"
		out["long_url"] = cl.LongURL
		out["redirect_type"] = cl.Redirect
	}
	writeJSON(w, http.StatusOK, out)
}

// emitClick sends the click to the event bus. Produce never blocks.
func (a *App) emitClick(r *http.Request, code string, userID int64, status int, hit bool) {
	info := a.enrich(r)
	a.bus.Produce(ClickEvent{
		TS: time.Now().UnixMilli(), Code: code, UserID: userID,
		Country: info.Country, City: info.City, Ref: info.Referrer,
		Device: info.Device, Browser: info.Browser, OS: info.OS, Bot: info.Bot,
		Status: status, Hit: hit,
	})
}
