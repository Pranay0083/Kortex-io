package main

// Turns a real redirect request into analytics dimensions: country/city,
// referrer host, device, browser, OS and a bot flag. Nothing is invented
// here; synthetic traffic lives in synthetic.go.

import (
	"net"
	"net/http"
	"strings"
)

// clickInfo holds the analytics dimensions taken from one request.
type clickInfo struct {
	Country, City, Referrer string
	Device, Browser, OS     string
	Bot                     bool
}

// enrich reads geo from Cloudflare's CF-IPCountry header or, if configured,
// a local GeoLite2 database. Unknown values stay "unknown".
func (a *App) enrich(r *http.Request) clickInfo {
	info := clickInfo{Country: "unknown", City: "unknown", Referrer: referrerOf(r)}
	info.Device, info.Browser, info.OS, info.Bot = parseUA(r.UserAgent())

	// "XX" and "T1" are Cloudflare's codes for unknown / Tor.
	if cc := strings.ToUpper(strings.TrimSpace(r.Header.Get("CF-IPCountry"))); len(cc) == 2 && cc != "XX" && cc != "T1" {
		info.Country = cc
	}
	if a.geo == nil {
		return info
	}
	ip := net.ParseIP(a.clientIP(r))
	if ip == nil {
		return info
	}
	rec, err := a.geo.City(ip)
	if err != nil {
		return info
	}
	if info.Country == "unknown" && rec.Country.IsoCode != "" {
		info.Country = rec.Country.IsoCode
	}
	// Only trust the city if it belongs to the country we settled on.
	if name := rec.City.Names["en"]; name != "" && rec.Country.IsoCode == info.Country {
		info.City = name
	}
	return info
}

// referrerOf returns the referring host without "www.", or "direct".
func referrerOf(r *http.Request) string {
	ref := r.Referer()
	if ref == "" {
		return "direct"
	}
	h := strings.TrimPrefix(hostOf(ref), "www.")
	if h == "" || len(h) > 253 {
		return "other"
	}
	return h
}

// --- User-Agent parsing --------------------------------------------------

// botTokens maps a lower-case UA substring to the client name we report.
var botTokens = []struct{ tok, name string }{
	{"curl/", "curl"}, {"wget/", "wget"}, {"python-requests", "python-requests"}, {"python-urllib", "python-urllib"},
	{"aiohttp", "aiohttp"}, {"go-http-client", "Go-http-client"}, {"k6/", "k6"}, {"headlesschrome", "HeadlessChrome"},
	{"phantomjs", "PhantomJS"}, {"okhttp", "okhttp"}, {"axios/", "axios"}, {"node-fetch", "node-fetch"},
	{"undici", "undici"}, {"java/", "Java"}, {"apache-httpclient", "Apache-HttpClient"}, {"libwww-perl", "libwww-perl"},
	{"httpie", "HTTPie"}, {"postman", "Postman"}, {"insomnia", "Insomnia"}, {"facebookexternalhit", "facebookexternalhit"},
	{"lighthouse", "Lighthouse"}, {"bot", "bot"}, {"crawler", "crawler"}, {"spider", "spider"}, {"slurp", "slurp"},
}

// parseUA is a small ordered matcher. Order matters: Edge and Opera also say
// "Chrome", and Chrome also says "Safari", so the more specific tokens are
// checked first. An empty UA is treated as a bot.
func parseUA(ua string) (device, browser, os string, bot bool) {
	l := strings.ToLower(strings.TrimSpace(ua))
	if l == "" {
		return "unknown", "unknown", "unknown", true
	}
	os = uaOS(l)
	for _, b := range botTokens {
		if strings.Contains(l, b.tok) {
			return "bot", b.name, os, true
		}
	}
	browser = uaBrowser(l)
	device = uaDevice(l, os, browser)
	if device != "desktop" && (browser == "Chrome" || browser == "Safari") {
		browser += " Mobile"
	}
	return device, browser, os, false
}

func uaOS(l string) string {
	switch {
	case strings.Contains(l, "windows phone"):
		return "Windows Phone"
	case strings.Contains(l, "ipad"):
		return "iPadOS"
	case strings.Contains(l, "iphone"), strings.Contains(l, "ipod"):
		return "iOS"
	case strings.Contains(l, "android"):
		return "Android"
	case strings.Contains(l, "cros"):
		return "ChromeOS"
	case strings.Contains(l, "windows"):
		return "Windows"
	case strings.Contains(l, "mac os x"), strings.Contains(l, "macintosh"):
		return "macOS"
	case strings.Contains(l, "linux"), strings.Contains(l, "x11"):
		return "Linux"
	}
	return "unknown"
}

func uaBrowser(l string) string {
	switch {
	case strings.Contains(l, "edg/"), strings.Contains(l, "edga/"), strings.Contains(l, "edgios/"):
		return "Edge"
	case strings.Contains(l, "opr/"), strings.Contains(l, "opera"), strings.Contains(l, "opt/"):
		return "Opera"
	case strings.Contains(l, "samsungbrowser"):
		return "Samsung Internet"
	case strings.Contains(l, "firefox/"), strings.Contains(l, "fxios/"):
		return "Firefox"
	case strings.Contains(l, "crios/"), strings.Contains(l, "chrome/"), strings.Contains(l, "chromium/"):
		return "Chrome"
	case strings.Contains(l, "safari/") && strings.Contains(l, "version/"):
		return "Safari"
	}
	return "other"
}

func uaDevice(l, os, browser string) string {
	switch {
	case os == "iPadOS" || strings.Contains(l, "tablet") || (os == "Android" && !strings.Contains(l, "mobile")):
		return "tablet"
	case os == "iOS" || os == "Windows Phone" || strings.Contains(l, "mobile"):
		return "mobile"
	case os == "unknown" && browser == "other":
		return "unknown"
	}
	return "desktop"
}
