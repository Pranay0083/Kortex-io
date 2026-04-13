package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	qrcode "github.com/skip2/go-qrcode"
)

type registerReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

type authResp struct {
	User         *User  `json:"user"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func (a *App) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerReq
	if readJSON(r, &req) != nil {
		writeErr(w, 400, "invalid payload")
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if !strings.Contains(req.Email, "@") || len(req.Password) < 8 {
		writeErr(w, 400, "valid email and 8+ character password required")
		return
	}
	if _, _, err := a.getUserByEmail(r.Context(), req.Email); err == nil {
		writeErr(w, 409, "an account with this email already exists")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = strings.Split(req.Email, "@")[0]
	}
	u, err := a.createUser(r.Context(), req.Email, req.Password, name, "user")
	if err != nil {
		writeErr(w, 500, "could not create account")
		return
	}
	a.emitAuth(w, u)
}

func (a *App) emitAuth(w http.ResponseWriter, u *User) {
	access, _ := a.issueToken(u, "access", 12*time.Hour)
	refresh, _ := a.issueToken(u, "refresh", 7*24*time.Hour)
	setAuthCookies(w, access, refresh)
	writeJSON(w, 200, authResp{User: u, AccessToken: access, RefreshToken: refresh})
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req registerReq
	if readJSON(r, &req) != nil {
		writeErr(w, 400, "invalid payload")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	ident := clientIP(r) + ":" + email
	if a.cache.FailedLogins(r.Context(), ident) >= 5 {
		writeErr(w, 429, "too many failed attempts — locked for 15 minutes")
		return
	}
	u, hash, err := a.getUserByEmail(r.Context(), email)
	if err != nil || !verifyPassword(req.Password, hash) {
		a.cache.BumpFailedLogin(r.Context(), ident)
		writeErr(w, 401, "invalid email or password")
		return
	}
	a.cache.ClearFailedLogin(r.Context(), ident)
	a.emitAuth(w, u)
}

func (a *App) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, currentUser(r))
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	clearAuthCookies(w)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) handleRefresh(w http.ResponseWriter, r *http.Request) {
	tok := ""
	if ck, err := r.Cookie("kortex_refresh"); err == nil {
		tok = ck.Value
	}
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = readJSON(r, &body)
	if body.RefreshToken != "" {
		tok = body.RefreshToken
	}
	c, err := a.parseToken(tok)
	if err != nil || c.Kind != "refresh" {
		writeErr(w, 401, "invalid refresh token")
		return
	}
	id, _ := atoi64(c.Subject)
	u, err := a.getUserByID(r.Context(), id)
	if err != nil {
		writeErr(w, 401, "user not found")
		return
	}
	a.emitAuth(w, u)
}

// --- links ---------------------------------------------------------------

type shortenReq struct {
	URL       string `json:"url"`
	Alias     string `json:"alias"`
	Title     string `json:"title"`
	ExpiresIn string `json:"expires_in"` // "", "1h", "24h", "7d", "30d"
	Permanent bool   `json:"permanent"`
}

type shortenResp struct {
	Link      *Link          `json:"link"`
	ShortURL  string         `json:"short_url"`
	PrettyURL string         `json:"pretty_url"`
	Timings   map[string]any `json:"timings"`
	Snowflake SnowflakeParts `json:"snowflake"`
}

func (a *App) handleShorten(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var req shortenReq
	if readJSON(r, &req) != nil {
		writeErr(w, 400, "invalid payload")
		return
	}
	ok, remaining, reset := a.cache.RateLimit(r.Context(), "shorten:"+itoa(u.ID), 40, time.Minute)
	w.Header().Set("X-RateLimit-Remaining", itoa(int64(remaining)))
	if !ok {
		writeErr(w, 429, "rate limit exceeded: 40 links/min — resets in "+itoa(int64(reset))+"s")
		return
	}

	long, valid := validURL(req.URL)
	if !valid {
		writeErr(w, 400, "that doesn't look like a valid http(s) URL")
		return
	}

	t0 := time.Now()
	id := a.sf.Next()
	tID := time.Since(t0)

	code := encodeCode(id)
	custom := false
	if alias := strings.TrimSpace(req.Alias); alias != "" {
		if !validAlias(alias) {
			writeErr(w, 400, "alias must be 3-32 chars: letters, digits, - or _")
			return
		}
		if reservedAliases[strings.ToLower(alias)] {
			writeErr(w, 409, "that alias is reserved")
			return
		}
		a.bloom.Queries.Add(1)
		if a.bloom.MayContain(alias) {
			a.bloom.Probes.Add(1)
			exists, err := a.store.CodeExists(r.Context(), alias)
			if err != nil {
				writeErr(w, 500, "shard lookup failed")
				return
			}
			if exists {
				writeErr(w, 409, "alias already taken")
				return
			}
			a.bloom.FalsePos.Add(1)
		} else {
			a.bloom.Skipped.Add(1)
		}
		code = alias
		custom = true
	}

	var expires *time.Time
	switch req.ExpiresIn {
	case "1h":
		t := time.Now().Add(time.Hour).UTC()
		expires = &t
	case "24h":
		t := time.Now().Add(24 * time.Hour).UTC()
		expires = &t
	case "7d":
		t := time.Now().Add(7 * 24 * time.Hour).UTC()
		expires = &t
	case "30d":
		t := time.Now().Add(30 * 24 * time.Hour).UTC()
		expires = &t
	}

	redirectType := 302
	if req.Permanent {
		redirectType = 301
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = hostOf(long)
	}

	l := &Link{
		ID: id, Code: code, LongURL: long, Title: title, UserID: u.ID, Custom: custom,
		Redirect: redirectType, ExpiresAt: expires, CreatedAt: time.Now().UTC(), Active: true,
	}
	t1 := time.Now()
	if err := a.store.InsertLink(r.Context(), l); err != nil {
		if strings.Contains(err.Error(), "duplicate") {
			writeErr(w, 409, "code collision — retry")
			return
		}
		writeErr(w, 500, "write to shard failed: "+err.Error())
		return
	}
	tPG := time.Since(t1)

	t2 := time.Now()
	a.cache.Set(r.Context(), l)
	tRedis := time.Since(t2)
	a.bloom.Add(code)
	a.olap.CodeID(code)

	writeJSON(w, 201, shortenResp{
		Link:      l,
		ShortURL:  a.publicBase(r) + "/api/r/" + code,
		PrettyURL: a.publicBase(r) + "/" + code,
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

func (a *App) publicBase(r *http.Request) string {
	if a.cfg.PublicBase != "" {
		return strings.TrimRight(a.cfg.PublicBase, "/")
	}
	if o := r.Header.Get("Origin"); o != "" {
		return strings.TrimRight(o, "/")
	}
	return "http://" + r.Host
}

func (a *App) handleListLinks(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	links, err := a.store.ListByUser(r.Context(), u.ID, qInt(r, "limit", 200))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(links))
	for _, l := range links {
		s := a.olap.Summary([]uint32{a.olap.CodeID(l.Code)})
		out = append(out, map[string]any{
			"link": l, "clicks_24h": s.Last24, "clicks_total": s.Total, "last_seen": s.Last,
		})
	}
	writeJSON(w, 200, map[string]any{"items": out, "count": len(out)})
}

func (a *App) handleGetLink(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	code := chi.URLParam(r, "code")
	l, err := a.store.GetByCode(r.Context(), code)
	if err != nil || l.UserID != u.ID {
		writeErr(w, 404, "link not found")
		return
	}
	writeJSON(w, 200, map[string]any{"link": l, "snowflake": explodeID(l.ID)})
}

func (a *App) handleDeleteLink(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	code := chi.URLParam(r, "code")
	ok, err := a.store.DeleteLink(r.Context(), code, u.ID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if !ok {
		writeErr(w, 404, "link not found")
		return
	}
	a.cache.Del(r.Context(), code)
	writeJSON(w, 200, map[string]bool{"deleted": true})
}

func (a *App) handleToggleLink(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	code := chi.URLParam(r, "code")
	l, err := a.store.GetByCode(r.Context(), code)
	if err != nil || l.UserID != u.ID {
		writeErr(w, 404, "link not found")
		return
	}
	if err := a.store.SetActive(r.Context(), code, u.ID, !l.Active); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	l.Active = !l.Active
	a.cache.Set(r.Context(), l)
	writeJSON(w, 200, map[string]any{"code": code, "active": l.Active})
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

func (a *App) handleCheckAlias(w http.ResponseWriter, r *http.Request) {
	alias := strings.TrimSpace(r.URL.Query().Get("alias"))
	res := aliasCheck{Alias: alias}
	if !validAlias(alias) {
		res.Reason = "3-32 chars, letters/digits/-/_ only"
		writeJSON(w, 200, res)
		return
	}
	res.Valid = true
	if reservedAliases[strings.ToLower(alias)] {
		res.Reserved = true
		res.Reason = "reserved path"
		writeJSON(w, 200, res)
		return
	}
	a.bloom.Queries.Add(1)
	t0 := time.Now()
	maybe := a.bloom.MayContain(alias)
	res.BloomNS = time.Since(t0).Nanoseconds()
	res.BloomMaybe = maybe
	res.BloomBits = a.bloom.SetBits(alias)
	if !maybe {
		a.bloom.Skipped.Add(1)
		res.Available = true
		res.Reason = "bloom says definitely-not-present — Postgres round trip skipped"
		writeJSON(w, 200, res)
		return
	}
	a.bloom.Probes.Add(1)
	t1 := time.Now()
	exists, err := a.store.CodeExists(r.Context(), alias)
	res.DBUS = time.Since(t1).Microseconds()
	res.DBChecked = true
	if err != nil {
		res.Reason = "shard lookup error"
		writeJSON(w, 200, res)
		return
	}
	res.Available = !exists
	if exists {
		res.Reason = "confirmed taken in shard " + itoa(int64(a.store.ShardFor(alias)))
	} else {
		a.bloom.FalsePos.Add(1)
		res.Reason = "bloom false positive — shard confirmed free"
	}
	writeJSON(w, 200, res)
}

func (a *App) handleQR(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	target := r.URL.Query().Get("url")
	if target == "" {
		target = "https://kortex.link/" + code
	}
	png, err := qrcode.Encode(target, qrcode.Medium, 512)
	if err != nil {
		writeErr(w, 500, "qr encode failed")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(png)
}

// --- the read path -------------------------------------------------------

func (a *App) resolve(ctx context.Context, code string) (*CachedLink, bool, bool) {
	if cl, ok := a.cache.Get(ctx, code); ok {
		return cl, true, true
	}
	l, err := a.store.GetByCode(ctx, code)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, false
		}
		return nil, false, false
	}
	a.cache.Set(ctx, l)
	cl := &CachedLink{Code: l.Code, LongURL: l.LongURL, UserID: l.UserID, Redirect: l.Redirect, Active: l.Active}
	if l.ExpiresAt != nil {
		cl.ExpiresAt = l.ExpiresAt.UnixMilli()
	}
	return cl, false, true
}

func (a *App) handleRedirect(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	cl, hit, found := a.resolve(r.Context(), code)
	if !found {
		a.emitClick(r, code, 0, 404, hit)
		writeErr(w, 404, "unknown short code")
		return
	}
	if !cl.Active {
		a.emitClick(r, code, cl.UserID, 410, hit)
		writeErr(w, 410, "link disabled")
		return
	}
	if cl.ExpiresAt > 0 && time.Now().UnixMilli() > cl.ExpiresAt {
		a.emitClick(r, code, cl.UserID, 410, hit)
		writeErr(w, 410, "link expired")
		return
	}
	a.store.BumpClicks(code, 1)
	a.emitClick(r, code, cl.UserID, cl.Redirect, hit)
	w.Header().Set("Location", cl.LongURL)
	w.Header().Set("Cache-Control", "private, max-age=0")
	if hit {
		w.Header().Set("X-Kortex-Cache", "HIT")
	} else {
		w.Header().Set("X-Kortex-Cache", "MISS")
	}
	w.WriteHeader(cl.Redirect)
}

func (a *App) handleResolveJSON(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	cl, hit, found := a.resolve(r.Context(), code)
	if !found {
		writeErr(w, 404, "unknown short code")
		return
	}
	writeJSON(w, 200, map[string]any{"long_url": cl.LongURL, "cache": map[string]bool{"hit": hit},
		"active": cl.Active, "redirect_type": cl.Redirect})
}

func (a *App) emitClick(r *http.Request, code string, userID int64, status int, hit bool) {
	rng := a.rngPool()
	country, city, ref, device, browser, os, bot := enrich(r, rng)
	a.bus.Produce(ClickEvent{
		TS: time.Now().UnixMilli(), Code: code, UserID: userID, Country: country, City: city,
		Ref: ref, Device: device, Browser: browser, OS: os, Bot: bot, IP: clientIP(r),
		Status: status, Hit: hit,
	})
}
