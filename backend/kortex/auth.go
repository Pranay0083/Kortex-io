package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type ctxKey string

const userCtxKey ctxKey = "kortex.user"

func hashPassword(p string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(p), 11)
	return string(b), err
}

func verifyPassword(plain, hashed string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plain)) == nil
}

type Claims struct {
	Email string `json:"email"`
	Kind  string `json:"kind"`
	jwt.RegisteredClaims
}

func (a *App) issueToken(u *User, kind string, ttl time.Duration) (string, error) {
	c := Claims{
		Email: u.Email,
		Kind:  kind,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   itoa(u.ID),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "kortex",
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(a.cfg.JWTSecret))
}

func (a *App) parseToken(tok string) (*Claims, error) {
	t, err := jwt.ParseWithClaims(tok, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(a.cfg.JWTSecret), nil
	})
	if err != nil {
		return nil, err
	}
	c, ok := t.Claims.(*Claims)
	if !ok || !t.Valid {
		return nil, errors.New("invalid token")
	}
	return c, nil
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	if ck, err := r.Cookie("kortex_access"); err == nil {
		return ck.Value
	}
	return ""
}

func (a *App) userFromRequest(r *http.Request) (*User, error) {
	tok := bearer(r)
	if tok == "" {
		return nil, errors.New("not authenticated")
	}
	c, err := a.parseToken(tok)
	if err != nil {
		return nil, err
	}
	if c.Kind != "access" {
		return nil, errors.New("wrong token kind")
	}
	id, err := atoi64(c.Subject)
	if err != nil {
		return nil, err
	}
	return a.getUserByID(r.Context(), id)
}

func (a *App) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := a.userFromRequest(r)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		ctx := context.WithValue(r.Context(), userCtxKey, u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func currentUser(r *http.Request) *User {
	u, _ := r.Context().Value(userCtxKey).(*User)
	return u
}

func (a *App) getUserByID(ctx context.Context, id int64) (*User, error) {
	var u User
	err := a.store.pool.QueryRow(ctx, `SELECT id, email, name, role, created_at FROM users WHERE id=$1`, id).
		Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (a *App) getUserByEmail(ctx context.Context, email string) (*User, string, error) {
	var u User
	var hash string
	err := a.store.pool.QueryRow(ctx, `SELECT id, email, name, role, created_at, password_hash FROM users WHERE email=$1`,
		strings.ToLower(strings.TrimSpace(email))).
		Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.CreatedAt, &hash)
	if err != nil {
		return nil, "", err
	}
	return &u, hash, nil
}

func (a *App) createUser(ctx context.Context, email, password, name, role string) (*User, error) {
	h, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	u := &User{
		ID:        a.sf.Next(),
		Email:     strings.ToLower(strings.TrimSpace(email)),
		Name:      name,
		Role:      role,
		CreatedAt: time.Now().UTC(),
	}
	_, err = a.store.pool.Exec(ctx,
		`INSERT INTO users (id,email,password_hash,name,role,created_at) VALUES ($1,$2,$3,$4,$5,$6)`,
		u.ID, u.Email, h, u.Name, u.Role, u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (a *App) seedAdmin(ctx context.Context) (*User, error) {
	u, hash, err := a.getUserByEmail(ctx, a.cfg.AdminEmail)
	if err == nil {
		if !verifyPassword(a.cfg.AdminPass, hash) {
			nh, _ := hashPassword(a.cfg.AdminPass)
			_, _ = a.store.pool.Exec(ctx, `UPDATE users SET password_hash=$2 WHERE id=$1`, u.ID, nh)
		}
		return u, nil
	}
	return a.createUser(ctx, a.cfg.AdminEmail, a.cfg.AdminPass, "Kortex Admin", "admin")
}

func setAuthCookies(w http.ResponseWriter, access, refresh string) {
	http.SetCookie(w, &http.Cookie{Name: "kortex_access", Value: access, Path: "/", HttpOnly: true,
		Secure: true, SameSite: http.SameSiteNoneMode, MaxAge: 43200})
	http.SetCookie(w, &http.Cookie{Name: "kortex_refresh", Value: refresh, Path: "/", HttpOnly: true,
		Secure: true, SameSite: http.SameSiteNoneMode, MaxAge: 604800})
}

func clearAuthCookies(w http.ResponseWriter) {
	for _, n := range []string{"kortex_access", "kortex_refresh"} {
		http.SetCookie(w, &http.Cookie{Name: n, Value: "", Path: "/", HttpOnly: true,
			Secure: true, SameSite: http.SameSiteNoneMode, MaxAge: -1})
	}
}
