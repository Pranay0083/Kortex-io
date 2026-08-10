package main

// Configuration comes from environment variables (a .env file is loaded in
// development). Every setting has a safe default except the database URLs
// and JWT_SECRET, which main refuses to start without.

import (
	"errors"
	"log"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port           string
	PGURL          string
	RedisURL       string
	JWTSecret      string
	ShardCount     int
	VNodes         int
	NodeID         int64
	NodeName       string
	Partitions     int
	CacheTTL       int
	AdminEmail     string
	AdminPass      string
	DemoEmail      string
	SeedEvents     int
	PublicBase     string
	StaticDir      string
	CORSOrigins    map[string]bool
	CookieSecure   bool
	DemoMode       bool
	TrustedProxies []netip.Prefix
	GeoIPDB        string
	RetentionDays  int
}

// env, envInt and envBool read a variable, falling back to def when it is
// unset or unparsable.
func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(k string, def bool) bool {
	if v := os.Getenv(k); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func loadConfig() Config {
	base := strings.TrimRight(env("PUBLIC_BASE_URL", ""), "/")
	return Config{
		Port:           env("KORTEX_PORT", "8090"),
		PGURL:          os.Getenv("POSTGRES_URL"),
		RedisURL:       os.Getenv("REDIS_URL"),
		JWTSecret:      os.Getenv("JWT_SECRET"),
		ShardCount:     envInt("SHARD_COUNT", 4),
		VNodes:         envInt("RING_VNODES", 160),
		NodeID:         int64(envInt("NODE_ID", 1)),
		NodeName:       env("NODE_NAME", "kortex-01"),
		Partitions:     envInt("BUS_PARTITIONS", 4),
		CacheTTL:       envInt("CACHE_TTL_SECONDS", 3600),
		AdminEmail:     env("ADMIN_EMAIL", defaultAdminEmail),
		AdminPass:      env("ADMIN_PASSWORD", defaultAdminPass),
		DemoEmail:      env("DEMO_EMAIL", "demo@kortex.dev"),
		SeedEvents:     envInt("SEED_EVENTS", 180000),
		PublicBase:     base,
		StaticDir:      env("STATIC_DIR", "/app/static"),
		CORSOrigins:    parseOrigins(os.Getenv("CORS_ORIGINS"), base),
		CookieSecure:   envBool("COOKIE_SECURE", strings.HasPrefix(base, "https://")),
		DemoMode:       envBool("DEMO_MODE", true),
		TrustedProxies: parseCIDRs(os.Getenv("TRUSTED_PROXIES")),
		GeoIPDB:        os.Getenv("GEOIP_DB"),
		RetentionDays:  envInt("ANALYTICS_RETENTION_DAYS", 30),
	}
}

// normOrigin lower-cases an origin and drops a trailing slash so that
// "https://A.com/" and "https://a.com" compare equal.
func normOrigin(o string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(o), "/"))
}

// parseOrigins builds the CORS allow-list. With CORS_ORIGINS unset it allows
// the public base URL and the local dev servers.
func parseOrigins(raw, base string) map[string]bool {
	out := map[string]bool{}
	if strings.TrimSpace(raw) == "" {
		raw = strings.Join([]string{base, "http://localhost:3000", "http://localhost:8090"}, ",")
	}
	for _, o := range strings.Split(raw, ",") {
		if o = normOrigin(o); o != "" {
			out[o] = true
		}
	}
	return out
}

// parseCIDRs accepts CIDRs or bare IPs; bad entries are logged and skipped.
func parseCIDRs(raw string) []netip.Prefix {
	var out []netip.Prefix
	for _, s := range strings.Split(raw, ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		if p, err := netip.ParsePrefix(s); err == nil {
			out = append(out, p.Masked())
			continue
		}
		if ip, err := netip.ParseAddr(s); err == nil {
			ip = ip.Unmap()
			out = append(out, netip.PrefixFrom(ip, ip.BitLen()))
			continue
		}
		log.Printf("TRUSTED_PROXIES: ignoring invalid entry %q", s)
	}
	return out
}

// Public demo credentials, used when ADMIN_EMAIL / ADMIN_PASSWORD are unset.
const (
	defaultAdminEmail = "admin@kortex.dev"
	defaultAdminPass  = "kortex2026"
)

var weakSecrets = map[string]bool{"change-this-secret": true, "change-me-to-a-random-hex-string": true}

func validateJWTSecret(s string) error {
	switch {
	case s == "":
		return errors.New("JWT_SECRET is not set — generate one with `openssl rand -hex 32`")
	case weakSecrets[s]:
		return errors.New("JWT_SECRET is still the placeholder value — generate one with `openssl rand -hex 32`")
	case len(s) < 32:
		return errors.New("JWT_SECRET must be at least 32 characters — generate one with `openssl rand -hex 32`")
	}
	return nil
}
