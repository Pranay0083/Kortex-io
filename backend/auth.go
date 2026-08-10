package main

// Authentication building blocks: password hashing, JWTs, cookies and the
// requireAuth middleware. The HTTP handlers are in handlers_auth.go.
//
// Session model:
//   - access token: JWT, valid 12h, sent as HttpOnly cookie (or Bearer header)
//   - refresh token: JWT, valid 7d, HttpOnly cookie scoped to /api/auth
//   - each token has a random ID (jti). Logout and refresh put the jti in a
//     Redis "revoked" set, so a refresh token works only once (rotation).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	accessTTL     = 12 * time.Hour
	refreshTTL    = 7 * 24 * time.Hour
	accessCookie  = "kortex_access"
	refreshCookie = "kortex_refresh"
	refreshPath   = "/api/auth" // the browser only sends the refresh cookie here
	tokenIssuer   = "kortex"
	bcryptCost    = 11

	kindAccess  = "access"
	kindRefresh = "refresh"
)

// --- passwords -----------------------------------------------------------

func hashPassword(p string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(p), bcryptCost)
	return string(b), err
}

func verifyPassword(plain, hashed string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plain)) == nil
}

// dummyHash lets a login for an unknown email pay the same bcrypt cost as a
// real one, so response time doesn't reveal which accounts exist.
var dummyHash = sync.OnceValue(func() string {
	h, _ := hashPassword("kortex-timing-equaliser")
	return h
})

// randomHex returns 32 hex chars of crypto randomness.
func randomHex() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// --- JWTs ----------------------------------------------------------------

type Claims struct {
	Email string `json:"email"`
	Kind  string `json:"kind"` // "access" or "refresh"
	jwt.RegisteredClaims
}

func (a *App) issueToken(u *User, kind string, ttl time.Duration) (string, error) {
	now := time.Now()
	c := Claims{
		Email: u.Email,
		Kind:  kind,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        randomHex(),
			Subject:   itoa(u.ID),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    tokenIssuer,
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(a.cfg.JWTSecret))
}

// parseToken checks signature, algorithm (HS256 only, which blocks
// "alg: none" tricks), issuer, expiry and that the token is the expected kind.
func (a *App) parseToken(tok, kind string) (*Claims, error) {
	t, err := jwt.ParseWithClaims(tok, &Claims{},
		func(*jwt.Token) (any, error) { return []byte(a.cfg.JWTSecret), nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(tokenIssuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, err
	}
	c, ok := t.Claims.(*Claims)
	if !ok || !t.Valid {
		return nil, errors.New("invalid token")
	}
	if c.Kind != kind || c.ID == "" {
		return nil, errors.New("wrong token kind")
	}
	return c, nil
}

// timeLeft is how long until the token expires; a revocation entry only
// needs to live that long.
func timeLeft(c *Claims) time.Duration {
	if c.ExpiresAt == nil {
		return time.Second
	}
	return time.Until(c.ExpiresAt.Time)
}

// bearer returns the access token from the Authorization header or cookie.
func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	if ck, err := r.Cookie(accessCookie); err == nil {
		return ck.Value
	}
	return ""
}

// --- middleware ----------------------------------------------------------

type ctxKey string

const userCtxKey ctxKey = "kortex.user"

func (a *App) userFromRequest(r *http.Request) (*User, error) {
	tok := bearer(r)
	if tok == "" {
		return nil, errors.New("not authenticated")
	}
	c, err := a.parseToken(tok, kindAccess)
	if err != nil {
		return nil, err
	}
	// If Redis is down we skip this check (fail open); access tokens are
	// short-lived anyway.
	if revoked, _ := a.cache.IsRevoked(r.Context(), c.ID); revoked {
		return nil, errors.New("token revoked")
	}
	id, err := atoi64(c.Subject)
	if err != nil {
		return nil, err
	}
	return a.store.GetUserByID(r.Context(), id)
}

// requireAuth rejects anonymous requests and puts the *User in the context.
func (a *App) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := a.userFromRequest(r)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userCtxKey, u)))
	})
}

// currentUser is the user set by requireAuth (nil on public routes).
func currentUser(r *http.Request) *User {
	u, _ := r.Context().Value(userCtxKey).(*User)
	return u
}

// --- cookies -------------------------------------------------------------

// emitAuth issues a fresh access/refresh pair as HttpOnly cookies and
// replies with the user. Tokens never appear in the JSON body, so page
// JavaScript (and any XSS) can't read them.
func (a *App) emitAuth(w http.ResponseWriter, u *User) {
	access, err1 := a.issueToken(u, kindAccess, accessTTL)
	refresh, err2 := a.issueToken(u, kindRefresh, refreshTTL)
	if err := errors.Join(err1, err2); err != nil {
		log.Printf("issue token: %v", err)
		writeErr(w, http.StatusInternalServerError, "could not issue session")
		return
	}
	http.SetCookie(w, a.cookie(accessCookie, access, "/", int(accessTTL.Seconds())))
	http.SetCookie(w, a.cookie(refreshCookie, refresh, refreshPath, int(refreshTTL.Seconds())))
	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (a *App) cookie(name, val, path string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name: name, Value: val, Path: path, MaxAge: maxAge,
		HttpOnly: true, Secure: a.cfg.CookieSecure, SameSite: http.SameSiteLaxMode,
	}
}

func (a *App) clearAuthCookies(w http.ResponseWriter) {
	http.SetCookie(w, a.cookie(accessCookie, "", "/", -1))
	http.SetCookie(w, a.cookie(refreshCookie, "", refreshPath, -1))
	// sessions issued before the refresh cookie was scoped to refreshPath
	http.SetCookie(w, a.cookie(refreshCookie, "", "/", -1))
}

// --- accounts ------------------------------------------------------------

func (a *App) createUser(ctx context.Context, email, password, name, role string) (*User, error) {
	h, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	u := &User{ID: a.ids.Next(), Email: normEmail(email), Name: name, Role: role, CreatedAt: time.Now().UTC()}
	if err := a.store.InsertUser(ctx, u, h); err != nil {
		return nil, err
	}
	return u, nil
}
