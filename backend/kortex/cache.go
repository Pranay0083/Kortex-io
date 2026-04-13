package main

import (
	"context"
	"encoding/json"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

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

func key(code string) string { return "k:l:" + code }

func (c *Cache) Get(ctx context.Context, code string) (*CachedLink, bool) {
	val, err := c.rdb.Get(ctx, key(code)).Bytes()
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

func (c *Cache) Set(ctx context.Context, l *Link) {
	cl := CachedLink{Code: l.Code, LongURL: l.LongURL, UserID: l.UserID, Redirect: l.Redirect, Active: l.Active}
	if l.ExpiresAt != nil {
		cl.ExpiresAt = l.ExpiresAt.UnixMilli()
	}
	b, _ := json.Marshal(cl)
	if err := c.rdb.Set(ctx, key(l.Code), b, c.ttl).Err(); err != nil {
		c.Errors.Add(1)
		return
	}
	c.Sets.Add(1)
}

func (c *Cache) Del(ctx context.Context, code string) { c.rdb.Del(ctx, key(code)) }

// Warm loads the hottest links so the first requests after a deploy don't
// stampede Postgres.
func (c *Cache) Warm(ctx context.Context, links []*Link, n int) int {
	pipe := c.rdb.Pipeline()
	count := 0
	for _, l := range links {
		if count >= n {
			break
		}
		cl := CachedLink{Code: l.Code, LongURL: l.LongURL, UserID: l.UserID, Redirect: l.Redirect, Active: l.Active}
		if l.ExpiresAt != nil {
			cl.ExpiresAt = l.ExpiresAt.UnixMilli()
		}
		b, _ := json.Marshal(cl)
		pipe.Set(ctx, key(l.Code), b, c.ttl)
		count++
	}
	_, _ = pipe.Exec(ctx)
	c.Warmed.Add(uint64(count))
	return count
}

// Token bucket via INCR + EXPIRE. Returns allowed, remaining, reset seconds.
func (c *Cache) RateLimit(ctx context.Context, bucket string, limit int, window time.Duration) (bool, int, int) {
	k := "k:rl:" + bucket
	n, err := c.rdb.Incr(ctx, k).Result()
	if err != nil {
		return true, limit, 0
	}
	if n == 1 {
		c.rdb.Expire(ctx, k, window)
	}
	ttl, _ := c.rdb.TTL(ctx, k).Result()
	remaining := limit - int(n)
	if remaining < 0 {
		remaining = 0
	}
	return int(n) <= limit, remaining, int(ttl.Seconds())
}

func (c *Cache) FailedLogins(ctx context.Context, id string) int {
	v, err := c.rdb.Get(ctx, "k:fail:"+id).Int()
	if err != nil {
		return 0
	}
	return v
}

func (c *Cache) BumpFailedLogin(ctx context.Context, id string) int {
	k := "k:fail:" + id
	n, _ := c.rdb.Incr(ctx, k).Result()
	if n == 1 {
		c.rdb.Expire(ctx, k, 15*time.Minute)
	}
	return int(n)
}

func (c *Cache) ClearFailedLogin(ctx context.Context, id string) { c.rdb.Del(ctx, "k:fail:"+id) }

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
	rate := 0.0
	if h+m > 0 {
		rate = float64(h) / float64(h+m) * 100
	}
	st := CacheStats{Hits: h, Misses: m, HitRate: rate, Sets: c.Sets.Load(), Warmed: c.Warmed.Load(), Errors: c.Errors.Load()}
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
	return st
}

func parseInfoInt(info, field string) int64 {
	idx := indexOf(info, field)
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

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
