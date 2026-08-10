package main

// Input rules for short codes, custom aliases and destination URLs.

import (
	"net/url"
	"regexp"
	"strings"
)

// aliasChars is a 256-entry lookup table: aliasChars[c] is true when byte c
// may appear in a short code. A table is faster than a regexp on the hot
// redirect path.
var aliasChars = func() [256]bool {
	var t [256]bool
	for _, c := range "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_" {
		t[byte(c)] = true
	}
	return t
}()

func onlyAliasChars(s string) bool {
	for i := 0; i < len(s); i++ {
		if !aliasChars[s[i]] {
			return false
		}
	}
	return true
}

// validAlias is the rule for a user-chosen alias: 3-32 chars of [A-Za-z0-9-_].
func validAlias(s string) bool {
	return len(s) >= 3 && len(s) <= 32 && onlyAliasChars(s)
}

// codeShape is the cheap pre-check on the redirect path. It is looser than
// validAlias because some seed aliases (like "k6") are shorter than 3 chars.
func codeShape(s string) bool {
	return len(s) >= 1 && len(s) <= 64 && onlyAliasChars(s)
}

// spaRoutes are the frontend's top-level pages. /{code} must never shadow them.
var spaRoutes = map[string]bool{"": true, "login": true, "register": true, "app": true, "lab": true, "architecture": true}

var reservedAliases = map[string]bool{
	"api": true, "app": true, "dashboard": true, "login": true, "register": true, "logout": true,
	"admin": true, "static": true, "assets": true, "lab": true, "bench": true, "docs": true,
	"architecture": true, "favicon": true, "manifest": true, "robots": true, "sitemap": true,
	"index": true, "health": true, "ops": true, "links": true,
}

// benchAliasPrefix names the admin-owned links the load generator targets.
const benchAliasPrefix = "bench-"

// isReservedAlias covers fixed app paths and the benchmark link namespace.
func isReservedAlias(alias string) bool {
	a := strings.ToLower(alias)
	return reservedAliases[a] || strings.HasPrefix(a, benchAliasPrefix)
}

const maxURLLen = 2048

var (
	hasSchemeRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://`)
	// Matches "javascript:", "data:", "mailto:" ... but not "host:8080/path".
	otherSchemeRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:[^0-9]`)
)

// validURL normalises a destination URL and reports whether it is allowed.
// Only http(s) URLs with a host and no embedded credentials are accepted;
// a bare "example.com/x" gets "https://" added.
func validURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxURLLen {
		return "", false
	}
	if !hasSchemeRe.MatchString(raw) {
		if otherSchemeRe.MatchString(raw) {
			return "", false
		}
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil {
		return "", false
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	out := u.String()
	if len(out) > maxURLLen {
		return "", false
	}
	return out, true
}

// hostOf returns the lower-cased host of a URL, or the input if it has none.
func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return raw
	}
	return strings.ToLower(u.Hostname())
}
