package main

// /api/auth/* handlers: register, login, demo login, refresh, logout, me.

import (
	"errors"
	"log"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const (
	authPerMinute    = 10               // register/login attempts per IP per minute
	maxFailedLogins  = 5                // per IP+email before lockout
	loginLockout     = 15 * time.Minute // how long the lockout lasts
	maxNameLen       = 80
	maxPassBytes     = 72 // bcrypt only looks at the first 72 bytes
	minPassBytes     = 8
	maxEmailLen      = 254
	msgAccountExists = "an account with this email already exists"
)

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

// internalErr logs the real error and gives the client nothing to fingerprint.
func (a *App) internalErr(w http.ResponseWriter, op string, err error) {
	log.Printf("%s: %v", op, err)
	writeErr(w, http.StatusInternalServerError, "internal error")
}

// authRateLimited enforces authPerMinute per client IP for one action, and
// writes the 429 itself.
func (a *App) authRateLimited(w http.ResponseWriter, r *http.Request, action string) bool {
	ok, _, reset := a.cache.RateLimit(r.Context(), action+":"+a.clientIP(r), authPerMinute, time.Minute)
	if !ok {
		writeErr(w, http.StatusTooManyRequests, "too many attempts — try again in "+itoa(int64(reset))+"s")
	}
	return !ok
}

// validEmail is stricter than net/mail: no display names, and the domain
// must contain a dot.
func validEmail(email string) bool {
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || len(email) > maxEmailLen {
		return false
	}
	domain := email[strings.LastIndex(email, "@")+1:]
	return strings.Contains(domain, ".")
}

func (a *App) handleRegister(w http.ResponseWriter, r *http.Request) {
	if a.authRateLimited(w, r, "register") {
		return
	}
	var req credentials
	if readJSON(r, &req) != nil {
		writeErr(w, http.StatusBadRequest, "invalid payload")
		return
	}
	email := normEmail(req.Email)
	if !validEmail(email) {
		writeErr(w, http.StatusBadRequest, "a valid email address is required")
		return
	}
	if len(req.Password) < minPassBytes || len(req.Password) > maxPassBytes {
		writeErr(w, http.StatusBadRequest, "password must be 8-72 characters")
		return
	}
	name := strings.TrimSpace(req.Name)
	if utf8.RuneCountInString(name) > maxNameLen {
		writeErr(w, http.StatusBadRequest, "name must be at most 80 characters")
		return
	}
	if name == "" {
		name = strings.Split(email, "@")[0]
	}

	if _, _, err := a.store.GetUserByEmail(r.Context(), email); err == nil {
		writeErr(w, http.StatusConflict, msgAccountExists)
		return
	}
	u, err := a.createUser(r.Context(), email, req.Password, name, "user")
	if isUniqueViolation(err) { // lost a race with a parallel signup
		writeErr(w, http.StatusConflict, msgAccountExists)
		return
	}
	if err != nil {
		a.internalErr(w, "create user", err)
		return
	}
	a.emitAuth(w, u)
}

// handleLogin has three defences: a per-IP rate limit, a per-IP+email
// lockout after 5 failures, and constant-time behaviour for unknown emails
// (bcrypt against a dummy hash) so attackers can't list accounts.
func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	if a.authRateLimited(w, r, "login") {
		return
	}
	var req credentials
	if readJSON(r, &req) != nil {
		writeErr(w, http.StatusBadRequest, "invalid payload")
		return
	}
	email := normEmail(req.Email)
	lockKey := a.clientIP(r) + ":" + email
	if a.cache.FailedLogins(r.Context(), lockKey) >= maxFailedLogins {
		writeErr(w, http.StatusTooManyRequests, "too many failed attempts — locked for 15 minutes")
		return
	}

	u, hash, err := a.store.GetUserByEmail(r.Context(), email)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		a.internalErr(w, "login lookup", err)
		return
	}
	if u == nil {
		hash = dummyHash()
	}
	if !verifyPassword(req.Password, hash) || u == nil {
		a.cache.BumpFailedLogin(r.Context(), lockKey)
		writeErr(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	a.cache.ClearFailedLogin(r.Context(), lockKey)
	a.emitAuth(w, u)
}

// handleDemoLogin signs the visitor into the shared, non-admin demo account.
// It works only while DEMO_MODE is on; the account has no usable password.
func (a *App) handleDemoLogin(w http.ResponseWriter, r *http.Request) {
	if !a.cfg.DemoMode || a.demoUserID == 0 {
		writeErr(w, http.StatusNotFound, "demo mode is off")
		return
	}
	if a.authRateLimited(w, r, "demo") {
		return
	}
	u, err := a.store.GetUserByID(r.Context(), a.demoUserID)
	if err != nil {
		a.internalErr(w, "demo login", err)
		return
	}
	a.emitAuth(w, u)
}

func (a *App) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, currentUser(r))
}

// handleLogout is public so an expired session can still be cleared. It
// revokes whatever valid tokens the request carries.
func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	revoke := func(tok, kind string) {
		if c, err := a.parseToken(tok, kind); err == nil {
			_, _ = a.cache.Revoke(r.Context(), c.ID, timeLeft(c))
		}
	}
	if ck, err := r.Cookie(refreshCookie); err == nil {
		revoke(ck.Value, kindRefresh)
	}
	if tok := bearer(r); tok != "" {
		revoke(tok, kindAccess)
	}
	a.clearAuthCookies(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleRefresh swaps a refresh token for a new access/refresh pair. Each
// refresh token works once: Revoke uses SETNX, which in one atomic step
// marks the jti as used and tells us whether it was already used. A second
// use means the token was stolen and replayed, so we log the client out.
func (a *App) handleRefresh(w http.ResponseWriter, r *http.Request) {
	ck, err := r.Cookie(refreshCookie)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	c, err := a.parseToken(ck.Value, kindRefresh)
	if err != nil {
		a.clearAuthCookies(w)
		writeErr(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	firstUse, err := a.cache.Revoke(r.Context(), c.ID, timeLeft(c))
	if err != nil {
		log.Printf("refresh revoke: %v", err)
		writeErr(w, http.StatusServiceUnavailable, "session store unavailable")
		return
	}
	if !firstUse {
		a.clearAuthCookies(w)
		writeErr(w, http.StatusUnauthorized, "refresh token already used")
		return
	}
	id, _ := atoi64(c.Subject)
	u, err := a.store.GetUserByID(r.Context(), id)
	if err != nil {
		a.clearAuthCookies(w)
		writeErr(w, http.StatusUnauthorized, "user not found")
		return
	}
	a.emitAuth(w, u)
}
