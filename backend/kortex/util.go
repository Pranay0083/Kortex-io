package main

import (
	"encoding/json"
	"math/rand"
	rand2 "math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// rnd lets the hot path use the lock-free per-P generator while seeding and
// benchmarks keep a deterministic *rand.Rand.
type rnd interface{ Intn(int) int }

type fastRand struct{}

func (fastRand) Intn(n int) int { return rand2.IntN(n) }

var _ rnd = (*rand.Rand)(nil)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"detail": msg})
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func atoi64(s string) (int64, error) { return strconv.ParseInt(s, 10, 64) }

func qInt(r *http.Request, k string, def int) int {
	if v := r.URL.Query().Get(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		return strings.TrimSpace(strings.Split(v, ",")[0])
	}
	if v := r.Header.Get("X-Real-Ip"); v != "" {
		return v
	}
	h := r.RemoteAddr
	if i := strings.LastIndex(h, ":"); i > 0 {
		return h[:i]
	}
	return h
}

var aliasOK = func() [256]bool {
	var t [256]bool
	for _, c := range "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_" {
		t[byte(c)] = true
	}
	return t
}()

func validAlias(s string) bool {
	if len(s) < 3 || len(s) > 32 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !aliasOK[s[i]] {
			return false
		}
	}
	return true
}

var reservedAliases = map[string]bool{
	"api": true, "app": true, "dashboard": true, "login": true, "register": true,
	"admin": true, "static": true, "assets": true, "lab": true, "bench": true, "docs": true,
}

func validURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	return u.String(), true
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return u.Host
}

// --- request enrichment (geo/device) -------------------------------------

var countries = []struct {
	Code   string
	Cities []string
	W      int
}{
	{"IN", []string{"Bengaluru", "Mumbai", "Delhi", "Hyderabad", "Pune"}, 28},
	{"US", []string{"San Francisco", "New York", "Austin", "Seattle", "Chicago"}, 24},
	{"DE", []string{"Berlin", "Munich", "Hamburg"}, 8},
	{"GB", []string{"London", "Manchester"}, 7},
	{"BR", []string{"São Paulo", "Rio de Janeiro"}, 6},
	{"SG", []string{"Singapore"}, 5},
	{"JP", []string{"Tokyo", "Osaka"}, 5},
	{"NG", []string{"Lagos", "Abuja"}, 4},
	{"ID", []string{"Jakarta", "Bandung"}, 4},
	{"FR", []string{"Paris", "Lyon"}, 3},
	{"AU", []string{"Sydney", "Melbourne"}, 3},
	{"CA", []string{"Toronto", "Vancouver"}, 3},
}

var referrers = []struct {
	Name string
	W    int
}{
	{"direct", 30}, {"x.com", 16}, {"news.ycombinator.com", 13}, {"google.com", 12},
	{"linkedin.com", 9}, {"reddit.com", 7}, {"github.com", 5}, {"whatsapp.com", 4},
	{"t.co", 2}, {"newsletter", 2},
}

var devices = []struct {
	Device, Browser, OS string
	W                   int
}{
	{"mobile", "Chrome Mobile", "Android", 34},
	{"mobile", "Safari Mobile", "iOS", 20},
	{"desktop", "Chrome", "macOS", 15},
	{"desktop", "Chrome", "Windows", 13},
	{"desktop", "Safari", "macOS", 6},
	{"desktop", "Firefox", "Linux", 5},
	{"desktop", "Edge", "Windows", 4},
	{"tablet", "Safari Mobile", "iPadOS", 3},
}

func pickCountry(rng rnd) (string, string) {
	total := 0
	for _, c := range countries {
		total += c.W
	}
	n := rng.Intn(total)
	for _, c := range countries {
		n -= c.W
		if n < 0 {
			return c.Code, c.Cities[rng.Intn(len(c.Cities))]
		}
	}
	return "US", "New York"
}

func pickReferrer(rng rnd) string {
	total := 0
	for _, r := range referrers {
		total += r.W
	}
	n := rng.Intn(total)
	for _, r := range referrers {
		n -= r.W
		if n < 0 {
			return r.Name
		}
	}
	return "direct"
}

func pickDevice(rng rnd) (string, string, string) {
	total := 0
	for _, d := range devices {
		total += d.W
	}
	n := rng.Intn(total)
	for _, d := range devices {
		n -= d.W
		if n < 0 {
			return d.Device, d.Browser, d.OS
		}
	}
	return "desktop", "Chrome", "Linux"
}

// Real UA parsing when a browser hits us; weighted synthesis for load traffic.
func enrich(r *http.Request, rng rnd) (country, city, ref, device, browser, os string, bot bool) {
	ua := r.UserAgent()
	ref = r.Referer()
	if ref == "" {
		ref = "direct"
	} else {
		ref = hostOf(ref)
	}
	l := strings.ToLower(ua)
	switch {
	case l == "" || strings.Contains(l, "bot") || strings.Contains(l, "curl") || strings.Contains(l, "k6"):
		bot = strings.Contains(l, "bot")
		device, browser, os = pickDevice(rng)
	case strings.Contains(l, "iphone"):
		device, browser, os = "mobile", "Safari Mobile", "iOS"
	case strings.Contains(l, "android"):
		device, browser, os = "mobile", "Chrome Mobile", "Android"
	case strings.Contains(l, "ipad"):
		device, browser, os = "tablet", "Safari Mobile", "iPadOS"
	case strings.Contains(l, "edg/"):
		device, browser, os = "desktop", "Edge", "Windows"
	case strings.Contains(l, "firefox"):
		device, browser, os = "desktop", "Firefox", "Linux"
	case strings.Contains(l, "chrome"):
		device, browser = "desktop", "Chrome"
		if strings.Contains(l, "mac os") {
			os = "macOS"
		} else if strings.Contains(l, "windows") {
			os = "Windows"
		} else {
			os = "Linux"
		}
	case strings.Contains(l, "safari"):
		device, browser, os = "desktop", "Safari", "macOS"
	default:
		device, browser, os = pickDevice(rng)
	}
	country, city = pickCountry(rng)
	return
}

func msAgo(d time.Duration) int64 { return time.Now().Add(-d).UnixMilli() }
