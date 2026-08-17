package main

// Small HTTP helpers shared by every handler: JSON in/out, query parsing,
// request-body limits and working out the real client IP behind proxies.

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr sends {"detail": msg}, the error shape the frontend expects.
func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"detail": msg})
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func atoi64(s string) (int64, error) { return strconv.ParseInt(s, 10, 64) }

// qInt reads an integer query parameter, falling back to def.
func qInt(r *http.Request, k string, def int) int {
	if v := r.URL.Query().Get(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func clamp(n, lo, hi int) int { return min(max(n, lo), hi) }

// qLimit reads ?limit= and keeps it between 1 and 500.
func qLimit(r *http.Request, def int) int { return clamp(qInt(r, "limit", def), 1, 500) }

// maxBody caps every /api request body at 64 KiB.
const maxBody = 64 << 10

func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		}
		next.ServeHTTP(w, r)
	})
}

// --- client IP -----------------------------------------------------------

// peerAddr is the IP of whoever opened the TCP connection.
func peerAddr(r *http.Request) netip.Addr {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = h
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return ip.Unmap()
}

func trusted(ip netip.Addr, proxies []netip.Prefix) bool {
	for _, p := range proxies {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// clientIP returns the address used for rate limiting and geo lookup.
//
// X-Forwarded-For is easy to fake, so it is only believed when the direct
// peer is one of our trusted proxies. Each proxy appends to the right of the
// header, so we walk it right-to-left and take the first hop that is not a
// trusted proxy: that is the last address a proxy we trust actually saw.
func clientIP(r *http.Request, proxies []netip.Prefix) string {
	peer := peerAddr(r)
	if !peer.IsValid() {
		return r.RemoteAddr
	}
	if !trusted(peer, proxies) {
		return peer.String()
	}
	if ip, ok := rightmostUntrusted(r.Header.Values("X-Forwarded-For"), proxies); ok {
		return ip.String()
	}
	if ip, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Real-Ip"))); err == nil {
		return ip.Unmap().String()
	}
	return peer.String()
}

// rightmostUntrusted walks the X-Forwarded-For hops from the right. If every
// hop is a trusted proxy it returns the left-most one it could parse.
func rightmostUntrusted(xff []string, proxies []netip.Prefix) (netip.Addr, bool) {
	if len(xff) == 0 {
		return netip.Addr{}, false
	}
	hops := strings.Split(strings.Join(xff, ","), ",")
	var last netip.Addr
	for i := len(hops) - 1; i >= 0; i-- {
		ip, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		last = ip.Unmap()
		if !trusted(last, proxies) {
			return last, true
		}
	}
	return last, last.IsValid()
}

func (a *App) clientIP(r *http.Request) string { return clientIP(r, a.cfg.TrustedProxies) }

// --- errors with a status ------------------------------------------------

// httpError is an error that already knows which status and message the
// client should see. Helpers return it so handlers can stay linear:
//
//	if err := a.check(...); err != nil { a.fail(w, "op", err); return }
type httpError struct {
	status int
	msg    string
}

func (e *httpError) Error() string { return e.msg }

func errStatus(status int, msg string) error { return &httpError{status, msg} }

// fail writes an httpError as is. Any other error is logged and becomes a
// generic 500.
func (a *App) fail(w http.ResponseWriter, op string, err error) {
	var he *httpError
	if errors.As(err, &he) {
		writeErr(w, he.status, he.msg)
		return
	}
	a.internalErr(w, op, err)
}
