package main

// Redis layer. It does four jobs:
//   - link cache: code -> CachedLink, so most redirects never touch Postgres
//   - rate limits: fixed-window counters per IP / user
//   - login lockout: failed-attempt counters
//   - token revocation: spent refresh-token IDs (single-use rotation)

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// CachedLink is the small JSON value stored per code. Short field names keep
// each Redis entry tiny.
type CachedLink struct {
	Code      string `json:"c"`
	LongURL   string `json:"u"`
	UserID    int64  `json:"o"`
	Redirect  int    `json:"r"`
	ExpiresAt int64  `json:"e"`
	Active    bool   `json:"a"`
}

type Cache struct {
	rdb    *redis.Client
	ttl    time.Duration
	Hits   atomic.Uint64
	Misses atomic.Uint64
	Sets   atomic.Uint64
	Errors atomic.Uint64
	Warmed atomic.Uint64

	// INFO/DBSIZE results, refreshed at most every statsTTL
	serverMu   sync.Mutex
	serverAt   time.Time
	serverSnap CacheStats
}

func NewCache(url string, ttlSeconds int) (*Cache, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	opt.PoolSize = 192
	opt.MinIdleConns = 32
	rdb := redis.NewClient(opt)
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		return nil, err
	}
	// allkeys-lru: hot links stay resident, cold tail is evicted under pressure.
	_ = rdb.ConfigSet(context.Background(), "maxmemory-policy", "allkeys-lru").Err()
	return &Cache{rdb: rdb, ttl: time.Duration(ttlSeconds) * time.Second}, nil
}

// statsTTL is how long the stats endpoints may reuse an expensive answer.
const statsTTL = 5 * time.Second

// Redis key prefixes.
const (
	keyLink        = "k:l:"    // link cache
	keyRateLimit   = "k:rl:"   // rate-limit window counter
	keyFailedLogin = "k:fail:" // failed login counter
	keyRevoked     = "k:rev:"  // revoked token id
)

func linkKey(code string) string { return keyLink + code }

func (c *Cache) Get(ctx context.Context, code string) (*CachedLink, bool) {
	val, err := c.rdb.Get(ctx, linkKey(code)).Bytes()
	if err != nil {
		if err != redis.Nil {
			c.Errors.Add(1)
		}
		c.Misses.Add(1)
		return nil, false
	}
	var cl CachedLink
	if json.Unmarshal(val, &cl) != nil {
		c.Misses.Add(1)
		return nil, false
	}
	c.Hits.Add(1)
	return &cl, true
}

func toCached(l *Link) *CachedLink {
	cl := &CachedLink{Code: l.Code, LongURL: l.LongURL, UserID: l.UserID, Redirect: l.Redirect, Active: l.Active}
	if l.ExpiresAt != nil {
		cl.ExpiresAt = l.ExpiresAt.UnixMilli()
	}
	return cl
}

func (cl *CachedLink) Expired(now time.Time) bool {
	return cl.ExpiresAt > 0 && now.UnixMilli() > cl.ExpiresAt
}

func (c *Cache) Set(ctx context.Context, l *Link) {
	b, _ := json.Marshal(toCached(l))
	if err := c.rdb.Set(ctx, linkKey(l.Code), b, c.ttl).Err(); err != nil {
		c.Errors.Add(1)
		return
	}
	c.Sets.Add(1)
}

func (c *Cache) Del(ctx context.Context, code string) { c.rdb.Del(ctx, linkKey(code)) }

// Warm preloads the first n links (callers pass them hottest first) in one
// pipelined round trip, so the first requests after a deploy don't all miss
// and hit Postgres at once.
func (c *Cache) Warm(ctx context.Context, links []*Link, n int) int {
	links = links[:min(n, len(links))]
	pipe := c.rdb.Pipeline()
	for _, l := range links {
		b, _ := json.Marshal(toCached(l))
		pipe.Set(ctx, linkKey(l.Code), b, c.ttl)
	}
	_, _ = pipe.Exec(ctx)
	c.Warmed.Add(uint64(len(links)))
	return len(links)
}

// incrWindow runs INCR and sets the expiry in one Lua script, which Redis
// runs atomically. As two separate calls, a crash in between could leave a
// counter with no TTL that never resets.
var incrWindow = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
local ttl = redis.call('PTTL', KEYS[1])
if ttl < 0 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
  ttl = tonumber(ARGV[1])
end
return {n, ttl}
`)

func (c *Cache) incr(ctx context.Context, k string, window time.Duration) (int64, time.Duration, error) {
	res, err := incrWindow.Run(ctx, c.rdb, []string{k}, window.Milliseconds()).Int64Slice()
	if err != nil || len(res) != 2 {
		return 0, 0, err
	}
	return res[0], time.Duration(res[1]) * time.Millisecond, nil
}

// RateLimit is a fixed-window counter: the first request starts a window of
// `window`, and up to `limit` requests are allowed until it expires. It
// returns allowed, remaining and seconds until reset. If Redis is down it
// fails open (allows the request) rather than taking the site down.
func (c *Cache) RateLimit(ctx context.Context, bucket string, limit int, window time.Duration) (allowed bool, remaining, resetS int) {
	n, ttl, err := c.incr(ctx, keyRateLimit+bucket, window)
	if err != nil {
		c.Errors.Add(1)
		return true, limit, 0
	}
	return int(n) <= limit, max(limit-int(n), 0), int(ttl.Seconds())
}

func (c *Cache) FailedLogins(ctx context.Context, id string) int {
	v, err := c.rdb.Get(ctx, keyFailedLogin+id).Int()
	if err != nil {
		return 0
	}
	return v
}

func (c *Cache) BumpFailedLogin(ctx context.Context, id string) int {
	n, _, _ := c.incr(ctx, keyFailedLogin+id, loginLockout)
	return int(n)
}

// Revoke marks a token id as spent until it would have expired anyway.
// It reports false if the id was already revoked (refresh-token replay).
func (c *Cache) Revoke(ctx context.Context, jti string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		ttl = time.Second
	}
	return c.rdb.SetNX(ctx, keyRevoked+jti, 1, ttl).Result()
}

func (c *Cache) IsRevoked(ctx context.Context, jti string) (bool, error) {
	n, err := c.rdb.Exists(ctx, keyRevoked+jti).Result()
	return n > 0, err
}

func (c *Cache) ClearFailedLogin(ctx context.Context, id string) { c.rdb.Del(ctx, keyFailedLogin+id) }

type CacheStats struct {
	Hits      uint64  `json:"hits"`
	Misses    uint64  `json:"misses"`
	HitRate   float64 `json:"hit_rate"`
	Sets      uint64  `json:"sets"`
	Warmed    uint64  `json:"warmed"`
	Errors    uint64  `json:"errors"`
	Keys      int64   `json:"keys"`
	UsedMemMB float64 `json:"used_memory_mb"`
	Policy    string  `json:"eviction_policy"`
	Evicted   int64   `json:"evicted_keys"`
}

func (c *Cache) Stats(ctx context.Context) CacheStats {
	h, m := c.Hits.Load(), c.Misses.Load()
	st := c.serverStats(ctx)
	st.Hits, st.Misses, st.HitRate = h, m, hitRate(h, m)
	st.Sets, st.Warmed, st.Errors = c.Sets.Load(), c.Warmed.Load(), c.Errors.Load()
	return st
}

// serverStats caches the DBSIZE/INFO/CONFIG round trips: the stats endpoint
// is public and polled by every open dashboard.
func (c *Cache) serverStats(ctx context.Context) CacheStats {
	c.serverMu.Lock()
	defer c.serverMu.Unlock()
	if time.Since(c.serverAt) < statsTTL {
		return c.serverSnap
	}
	var st CacheStats
	if n, err := c.rdb.DBSize(ctx).Result(); err == nil {
		st.Keys = n
	}
	if info, err := c.rdb.Info(ctx, "memory", "stats").Result(); err == nil {
		st.UsedMemMB = float64(parseInfoInt(info, "used_memory:")) / 1024 / 1024
		st.Evicted = parseInfoInt(info, "evicted_keys:")
	}
	if v, err := c.rdb.ConfigGet(ctx, "maxmemory-policy").Result(); err == nil {
		st.Policy = v["maxmemory-policy"]
	}
	c.serverSnap, c.serverAt = st, time.Now()
	return st
}

// hitRate is hits as a percentage of all lookups.
func hitRate(hits, misses uint64) float64 {
	if hits+misses == 0 {
		return 0
	}
	return float64(hits) / float64(hits+misses) * 100
}

// parseInfoInt pulls "field:<number>" out of Redis INFO output.
func parseInfoInt(info, field string) int64 {
	idx := strings.Index(info, field)
	if idx < 0 {
		return 0
	}
	start := idx + len(field)
	end := start
	for end < len(info) && info[end] != '\r' && info[end] != '\n' {
		end++
	}
	n, _ := strconv.ParseInt(info[start:end], 10, 64)
	return n
}
