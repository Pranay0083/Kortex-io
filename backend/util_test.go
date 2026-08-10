package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidAlias(t *testing.T) {
	for s, want := range map[string]bool{
		"abc": true, "my-link_2": true, strings.Repeat("a", 32): true,
		"ab": false, strings.Repeat("a", 33): false, "has space": false, "dot.com": false, "ünï": false, "": false,
	} {
		if got := validAlias(s); got != want {
			t.Errorf("validAlias(%q) = %v", s, got)
		}
	}
	if !codeShape("k6") || codeShape("") || codeShape("a.b") || codeShape(strings.Repeat("a", 65)) {
		t.Error("codeShape")
	}
}

func TestValidURL(t *testing.T) {
	ok := map[string]string{
		"https://example.com":         "https://example.com",
		"example.com":                 "https://example.com",
		"example.com/?u=https://x.io": "https://example.com/?u=https://x.io",
		"HTTP://Example.com/a":        "http://Example.com/a",
		"localhost:3000/path":         "https://localhost:3000/path",
		"http://10.0.0.1:8080/x?y=1":  "http://10.0.0.1:8080/x?y=1",
	}
	for in, want := range ok {
		got, valid := validURL(in)
		if !valid || got != want {
			t.Errorf("validURL(%q) = %q, %v; want %q", in, got, valid, want)
		}
	}
	for _, in := range []string{
		"", "   ", "javascript:alert(1)", "JavaScript://%0aalert(1)", "data:text/html,hi", "mailto:a@b.c",
		"ftp://example.com", "file:///etc/passwd", "https://", "//example.com", "https://user:pw@evil.com",
		"https://example.com/" + strings.Repeat("a", maxURLLen),
	} {
		if got, valid := validURL(in); valid {
			t.Errorf("validURL(%q) accepted as %q", in, got)
		}
	}
}

func TestClientIP(t *testing.T) {
	proxies := parseCIDRs("10.0.0.0/8, 192.168.1.1, ::1, garbage")
	if len(proxies) != 3 {
		t.Fatalf("parsed %d proxies", len(proxies))
	}
	cases := []struct {
		name, remote, xff, real string
		trusted                 bool
		want                    string
	}{
		{"no proxy config ignores xff", "203.0.113.9:5555", "1.2.3.4", "", false, "203.0.113.9"},
		{"untrusted peer ignores xff", "203.0.113.9:5555", "1.2.3.4", "5.6.7.8", true, "203.0.113.9"},
		{"trusted peer uses xff", "10.1.2.3:443", "198.51.100.7", "", true, "198.51.100.7"},
		{"spoofed left-most entry skipped", "10.1.2.3:443", "6.6.6.6, 198.51.100.7, 10.9.9.9", "", true, "198.51.100.7"},
		{"x-real-ip from trusted peer", "192.168.1.1:80", "", "198.51.100.8", true, "198.51.100.8"},
		{"ipv6 peer", "[2001:db8::1]:8080", "1.2.3.4", "", true, "2001:db8::1"},
		{"ipv6 trusted loopback", "[::1]:8080", "2001:db8::5", "", true, "2001:db8::5"},
		{"garbage xff falls back", "10.1.2.3:443", "not-an-ip", "", true, "10.1.2.3"},
		{"bare remote addr", "198.51.100.1", "", "", false, "198.51.100.1"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if c.real != "" {
			r.Header.Set("X-Real-Ip", c.real)
		}
		p := proxies
		if !c.trusted {
			p = nil
		}
		if got := clientIP(r, p); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestParseUA(t *testing.T) {
	cases := []struct {
		ua, device, browser, os string
		bot                     bool
	}{
		{"", "unknown", "unknown", "unknown", true},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36", "desktop", "Chrome", "Windows", false},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36 Edg/126.0.0.0", "desktop", "Edge", "macOS", false},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 14.5; rv:127.0) Gecko/20100101 Firefox/127.0", "desktop", "Firefox", "macOS", false},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:127.0) Gecko/20100101 Firefox/127.0", "desktop", "Firefox", "Windows", false},
		{"Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:127.0) Gecko/20100101 Firefox/127.0", "desktop", "Firefox", "Linux", false},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15", "desktop", "Safari", "macOS", false},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1", "mobile", "Safari Mobile", "iOS", false},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/126.0.6478.54 Mobile/15E148 Safari/604.1", "mobile", "Chrome Mobile", "iOS", false},
		{"Mozilla/5.0 (iPad; CPU OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1", "tablet", "Safari Mobile", "iPadOS", false},
		{"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36", "mobile", "Chrome Mobile", "Android", false},
		{"Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/25.0 Chrome/121.0.0.0 Mobile Safari/537.36", "mobile", "Samsung Internet", "Android", false},
		{"Mozilla/5.0 (Linux; Android 13; SM-X700) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36", "tablet", "Chrome Mobile", "Android", false},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36 OPR/111.0.0.0", "desktop", "Opera", "Windows", false},
		{"Mozilla/5.0 (X11; CrOS x86_64 14541.0.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36", "desktop", "Chrome", "ChromeOS", false},
		{"curl/8.6.0", "bot", "curl", "unknown", true},
		{"Wget/1.21.4", "bot", "wget", "unknown", true},
		{"python-requests/2.32.3", "bot", "python-requests", "unknown", true},
		{"Go-http-client/1.1", "bot", "Go-http-client", "unknown", true},
		{"k6/0.52 kortex-loadgen", "bot", "k6", "unknown", true},
		{"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/126.0.0.0 Safari/537.36", "bot", "HeadlessChrome", "Linux", true},
		{"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", "bot", "bot", "unknown", true},
		{"Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)", "bot", "bot", "unknown", true},
		{"SomethingWeird/1.0", "unknown", "other", "unknown", false},
	}
	for _, c := range cases {
		d, b, o, bot := parseUA(c.ua)
		if d != c.device || b != c.browser || o != c.os || bot != c.bot {
			t.Errorf("parseUA(%q) = %s/%s/%s/%v; want %s/%s/%s/%v", c.ua, d, b, o, bot, c.device, c.browser, c.os, c.bot)
		}
	}
}

func TestEnrichRealRequest(t *testing.T) {
	a := &App{}
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("User-Agent", "curl/8")
	r.Header.Set("Referer", "https://www.news.ycombinator.com/item?id=1")
	info := a.enrich(r)
	if info.Country != "unknown" || info.City != "unknown" || info.Referrer != "news.ycombinator.com" || !info.Bot {
		t.Fatalf("enrich = %+v", info)
	}
	r.Header.Set("CF-IPCountry", "de")
	if country := a.enrich(r).Country; country != "DE" {
		t.Fatalf("cf country = %s", country)
	}
	r.Header.Set("CF-IPCountry", "XX")
	if country := a.enrich(r).Country; country != "unknown" {
		t.Fatalf("XX should be unknown, got %s", country)
	}
}

func TestConfigHelpers(t *testing.T) {
	for s, ok := range map[string]bool{
		"": false, "change-this-secret": false, "change-me-to-a-random-hex-string": false,
		"short": false, strings.Repeat("x", 32): true,
	} {
		if err := validateJWTSecret(s); (err == nil) != ok {
			t.Errorf("validateJWTSecret(%q) err=%v", s, err)
		}
	}
	o := parseOrigins("", "https://kortex.example/")
	if !o["https://kortex.example"] || !o["http://localhost:3000"] || !o["http://localhost:8090"] || len(o) != 3 {
		t.Fatalf("default origins %v", o)
	}
	if o := parseOrigins("https://A.com/, https://b.com", "https://ignored"); !o["https://a.com"] || !o["https://b.com"] || o["https://ignored"] {
		t.Fatalf("explicit origins %v", o)
	}
}
