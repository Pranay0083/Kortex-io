package main

// Link management (the write path): create, list, get, delete, toggle,
// alias availability and QR codes. The redirect read path is in
// handlers_redirect.go.

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	qrcode "github.com/skip2/go-qrcode"
)

const (
	shortenPerMinute = 40
	maxTitleLen      = 200
)

// expiryOptions are the allowed values of shortenReq.ExpiresIn.
var expiryOptions = map[string]time.Duration{
	"1h":  time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
}

type shortenReq struct {
	URL       string `json:"url"`
	Alias     string `json:"alias"`
	Title     string `json:"title"`
	ExpiresIn string `json:"expires_in"` // "", "1h", "24h", "7d", "30d"
	Permanent bool   `json:"permanent"`  // 301 instead of 302
}

type shortenResp struct {
	Link      *Link          `json:"link"`
	ShortURL  string         `json:"short_url"`
	PrettyURL string         `json:"pretty_url"`
	Timings   map[string]any `json:"timings"`
	Snowflake SnowflakeParts `json:"snowflake"`
}

// handleShorten creates a short link:
//  1. rate-limit per user
//  2. validate URL and title
//  3. new Snowflake ID -> code (or the user's alias, checked via Bloom + DB)
//  4. INSERT into the owning shard, then write-through to Redis
//
// It reports how long each step took so the UI can show the write path.
func (a *App) handleShorten(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var req shortenReq
	if readJSON(r, &req) != nil {
		writeErr(w, http.StatusBadRequest, "invalid payload")
		return
	}
	ok, remaining, reset := a.cache.RateLimit(r.Context(), "shorten:"+itoa(u.ID), shortenPerMinute, time.Minute)
	w.Header().Set("X-RateLimit-Remaining", itoa(int64(remaining)))
	if !ok {
		writeErr(w, http.StatusTooManyRequests, "rate limit exceeded: 40 links/min — resets in "+itoa(int64(reset))+"s")
		return
	}

	long, title, err := a.validateTarget(req.URL, req.Title)
	if err != nil {
		a.fail(w, "validate", err)
		return
	}

	t0 := time.Now()
	id := a.ids.Next()
	tID := time.Since(t0)

	code, custom := encodeCode(id), false
	if alias := strings.TrimSpace(req.Alias); alias != "" {
		if err := a.checkAliasFree(r.Context(), alias); err != nil {
			a.fail(w, "alias lookup", err)
			return
		}
		code, custom = alias, true
	}

	l := &Link{
		ID: id, Code: code, LongURL: long, Title: title, UserID: u.ID, Custom: custom,
		Redirect: http.StatusFound, CreatedAt: time.Now().UTC(), Active: true,
	}
	if req.Permanent {
		l.Redirect = http.StatusMovedPermanently
	}
	if d, ok := expiryOptions[req.ExpiresIn]; ok {
		t := time.Now().Add(d).UTC()
		l.ExpiresAt = &t
	}

	t1 := time.Now()
	if err := a.store.InsertLink(r.Context(), l); err != nil {
		switch {
		case isUniqueViolation(err) && custom:
			writeErr(w, http.StatusConflict, "alias already taken") // lost a race
		case isUniqueViolation(err):
			writeErr(w, http.StatusConflict, "code collision — retry")
		default:
			a.internalErr(w, "insert link", err)
		}
		return
	}
	tPG := time.Since(t1)

	t2 := time.Now()
	a.cache.Set(r.Context(), l)
	tRedis := time.Since(t2)
	a.bloom.Add(code)

	base := a.publicBase(r)
	writeJSON(w, http.StatusCreated, shortenResp{
		Link:      l,
		ShortURL:  base + "/api/r/" + code,
		PrettyURL: base + "/" + code,
		Timings: map[string]any{
			"id_gen_us":   tID.Microseconds(),
			"pg_write_us": tPG.Microseconds(),
			"redis_us":    tRedis.Microseconds(),
			"shard":       l.Shard,
			"total_us":    time.Since(t0).Microseconds(),
		},
		Snowflake: explodeID(id),
	})
}

// validateTarget checks the destination URL and title, returning the
// normalised URL and the title to store (the URL's host if none was given).
func (a *App) validateTarget(rawURL, rawTitle string) (long, title string, err error) {
	if len(strings.TrimSpace(rawURL)) > maxURLLen {
		return "", "", errStatus(http.StatusBadRequest, "URL must be at most 2048 characters")
	}
	long, ok := validURL(rawURL)
	if !ok {
		return "", "", errStatus(http.StatusBadRequest, "that doesn't look like a valid http(s) URL")
	}
	if a.isSelfURL(long) {
		return "", "", errStatus(http.StatusBadRequest, "can't shorten a link to this service (redirect loop)")
	}
	title = strings.TrimSpace(rawTitle)
	if utf8.RuneCountInString(title) > maxTitleLen {
		return "", "", errStatus(http.StatusBadRequest, "title must be at most 200 characters")
	}
	if title == "" {
		title = hostOf(long)
	}
	return long, title, nil
}

// checkAliasFree returns nil if alias is valid, not reserved and unused.
func (a *App) checkAliasFree(ctx context.Context, alias string) error {
	if !validAlias(alias) {
		return errStatus(http.StatusBadRequest, "alias must be 3-32 chars: letters, digits, - or _")
	}
	if isReservedAlias(alias) {
		return errStatus(http.StatusConflict, "that alias is reserved")
	}
	if a.bloomRulesOut(alias) {
		return nil
	}
	exists, err := a.store.CodeExists(ctx, alias)
	if err != nil {
		return err
	}
	if exists {
		return errStatus(http.StatusConflict, "alias already taken")
	}
	a.bloom.FalsePos.Add(1)
	return nil
}

// bloomRulesOut asks the Bloom filter about code and updates its counters.
// true means code definitely does not exist, so the database can be skipped.
// false means "maybe": the caller must check the shard, and should count a
// FalsePos if the shard says no.
func (a *App) bloomRulesOut(code string) bool {
	a.bloom.Queries.Add(1)
	if !a.bloom.MayContain(code) {
		a.bloom.Skipped.Add(1)
		return true
	}
	a.bloom.Probes.Add(1)
	return false
}

// publicBase is the origin used in short URLs: PUBLIC_BASE_URL if set, then
// an allowed request Origin, then the Host header.
func (a *App) publicBase(r *http.Request) string {
	if a.cfg.PublicBase != "" {
		return a.cfg.PublicBase
	}
	if o := normOrigin(r.Header.Get("Origin")); a.cfg.CORSOrigins[o] {
		return o
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// isSelfURL stops users from shortening our own URLs (redirect loops).
func (a *App) isSelfURL(long string) bool {
	if a.cfg.PublicBase == "" {
		return false
	}
	u, err := url.Parse(long)
	b, err2 := url.Parse(a.cfg.PublicBase)
	return err == nil && err2 == nil && strings.EqualFold(u.Hostname(), b.Hostname())
}

// isDemo reports whether code is one of the protected seed links.
func (a *App) isDemo(code string) bool { return a.cfg.DemoMode && a.demoCodes[code] }

func (a *App) handleListLinks(w http.ResponseWriter, r *http.Request) {
	links, err := a.store.ListByUser(r.Context(), currentUser(r).ID, qLimit(r, 200))
	if err != nil {
		a.internalErr(w, "list links", err)
		return
	}
	ids := make([]uint32, len(links))
	for i, l := range links {
		ids[i] = a.olap.CodeID(l.Code)
	}
	sums := a.olap.SummaryBatch(ids)
	items := make([]map[string]any, len(links))
	for i, l := range links {
		items[i] = map[string]any{
			"link": l, "clicks_24h": sums[i].Last24, "clicks_total": sums[i].Total, "last_seen": sums[i].Last,
			"demo": a.isDemo(l.Code),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

// ownedLink loads code and checks the caller owns it. Someone else's link
// gets the same 404 as a missing one, so codes can't be probed for owners.
func (a *App) ownedLink(r *http.Request, code string) (*Link, error) {
	l, err := a.store.GetByCode(r.Context(), code)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if err != nil || l.UserID != currentUser(r).ID {
		return nil, errStatus(http.StatusNotFound, "link not found")
	}
	return l, nil
}

func (a *App) handleGetLink(w http.ResponseWriter, r *http.Request) {
	l, err := a.ownedLink(r, chi.URLParam(r, "code"))
	if err != nil {
		a.fail(w, "get link", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"link": l, "snowflake": explodeID(l.ID), "demo": a.isDemo(l.Code)})
}

// handleDeleteLink removes the row, then clears every other place the code
// lives: Redis, pending click counts and analytics.
func (a *App) handleDeleteLink(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	if a.isDemo(code) {
		writeErr(w, http.StatusForbidden, "demo links are protected")
		return
	}
	deleted, err := a.store.DeleteLink(r.Context(), code, currentUser(r).ID)
	if err != nil {
		a.internalErr(w, "delete link", err)
		return
	}
	if !deleted {
		writeErr(w, http.StatusNotFound, "link not found")
		return
	}
	a.cache.Del(r.Context(), code)
	a.store.DropPending(code)
	a.olap.Purge(code)
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (a *App) handleToggleLink(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	if a.isDemo(code) {
		writeErr(w, http.StatusForbidden, "demo links are protected")
		return
	}
	l, err := a.store.ToggleActive(r.Context(), code, currentUser(r).ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "link not found")
		return
	}
	if err != nil {
		a.internalErr(w, "toggle link", err)
		return
	}
	a.cache.Set(r.Context(), l) // write-through so redirects see the change immediately
	writeJSON(w, http.StatusOK, map[string]any{"code": code, "active": l.Active})
}

type aliasCheck struct {
	Alias      string   `json:"alias"`
	Valid      bool     `json:"valid"`
	Reserved   bool     `json:"reserved"`
	BloomMaybe bool     `json:"bloom_maybe"`
	BloomBits  []uint64 `json:"bloom_bits"`
	DBChecked  bool     `json:"db_checked"`
	Available  bool     `json:"available"`
	BloomNS    int64    `json:"bloom_ns"`
	DBUS       int64    `json:"db_us"`
	Reason     string   `json:"reason"`
}

// handleCheckAlias is checkAliasFree with every step reported, for the
// live "is this alias free?" hint in the UI.
func (a *App) handleCheckAlias(w http.ResponseWriter, r *http.Request) {
	alias := strings.TrimSpace(r.URL.Query().Get("alias"))
	res := aliasCheck{Alias: alias}
	defer func() { writeJSON(w, http.StatusOK, res) }()

	if !validAlias(alias) {
		res.Reason = "3-32 chars, letters/digits/-/_ only"
		return
	}
	res.Valid = true
	if isReservedAlias(alias) {
		res.Reserved = true
		res.Reason = "reserved path"
		return
	}

	t0 := time.Now()
	ruledOut := a.bloomRulesOut(alias)
	res.BloomNS = time.Since(t0).Nanoseconds()
	res.BloomMaybe = !ruledOut
	res.BloomBits = a.bloom.SetBits(alias)
	if ruledOut {
		res.Available = true
		res.Reason = "bloom says definitely-not-present — Postgres round trip skipped"
		return
	}

	t1 := time.Now()
	exists, err := a.store.CodeExists(r.Context(), alias)
	res.DBUS = time.Since(t1).Microseconds()
	res.DBChecked = true
	switch {
	case err != nil:
		res.Reason = "shard lookup error"
	case exists:
		res.Reason = "confirmed taken in shard " + itoa(int64(a.store.ShardFor(alias)))
	default:
		a.bloom.FalsePos.Add(1)
		res.Available = true
		res.Reason = "bloom false positive — shard confirmed free"
	}
}

func (a *App) handleQR(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("url")
	if target == "" {
		target = a.publicBase(r) + "/" + chi.URLParam(r, "code")
	}
	if len(target) > maxURLLen {
		writeErr(w, http.StatusBadRequest, "url too long")
		return
	}
	png, err := qrcode.Encode(target, qrcode.Medium, 512)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "qr encode failed")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(png)
}
