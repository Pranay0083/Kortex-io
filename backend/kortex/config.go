package main

import (
	"os"
	"strconv"
)

type Config struct {
	Port       string
	PGURL      string
	RedisURL   string
	JWTSecret  string
	ShardCount int
	VNodes     int
	NodeID     int64
	Partitions int
	CacheTTL   int
	AdminEmail string
	AdminPass  string
	SeedEvents int
	PublicBase string
}

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

func loadConfig() Config {
	return Config{
		Port:       env("KORTEX_PORT", "8090"),
		PGURL:      os.Getenv("POSTGRES_URL"),
		RedisURL:   os.Getenv("REDIS_URL"),
		JWTSecret:  os.Getenv("JWT_SECRET"),
		ShardCount: envInt("SHARD_COUNT", 4),
		VNodes:     envInt("RING_VNODES", 160),
		NodeID:     int64(envInt("NODE_ID", 1)),
		Partitions: envInt("BUS_PARTITIONS", 4),
		CacheTTL:   envInt("CACHE_TTL_SECONDS", 3600),
		AdminEmail: env("ADMIN_EMAIL", "admin@kortex.dev"),
		AdminPass:  env("ADMIN_PASSWORD", "kortex2026"),
		SeedEvents: envInt("SEED_EVENTS", 180000),
		PublicBase: env("PUBLIC_BASE_URL", ""),
	}
}
