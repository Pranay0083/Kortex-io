# Backend code tour

One Go binary (`package main`). Every component is a struct hung off `App`
(main.go); HTTP handlers are methods on `*App`.

## Where things live

| File | What it is | Key idea |
|---|---|---|
| `main.go` | startup, wiring, graceful shutdown | connect with retry → start workers → seed → warm cache → serve → drain on SIGTERM |
| `routes.go` | URL table, CORS, SPA fallback | public read path vs `requireAuth` group |
| `config.go` | env vars → `Config` | safe defaults; refuses a weak `JWT_SECRET` |
| `idgen.go` | Snowflake IDs + short codes | 41b time / 10b node / 12b seq; code = base62(id × odd constant) |
| `shard.go` | consistent hash ring | vnodes; adding a shard moves ~1/N keys, not ~all |
| `bloom.go` | Bloom filter | "definitely not" skips Postgres; tunable false-positive rate |
| `cache.go` | Redis | link cache, rate limits, login lockout, token revocation |
| `pg.go` | Postgres (all SQL) | `links_s0..N` shard tables; batched click-counter flush |
| `bus.go` | in-process Kafka-like bus | non-blocking produce, partitions by code, batched consumers |
| `olap.go` | columnar analytics | day partitions, dictionary encoding, background merge, materialized views |
| `auth.go` / `handlers_auth.go` | JWT sessions | HttpOnly cookies, single-use refresh tokens |
| `handlers_redirect.go` | **hot path** | Redis → Bloom → shard → 30x |
| `handlers_links.go` | create / list / delete / toggle | validation, alias check, write-through cache |
| `handlers_analytics.go` | dashboard queries | `?mode=raw` vs MV to compare |
| `handlers_lab.go` | ops stats + interactive demos | |
| `bench.go` | built-in load test | lock-free latency histogram |
| `seed.go` | demo data | rejection-sampled realistic traffic |
| `httputil.go`, `validate.go`, `useragent.go`, `synthetic.go` | helpers | |

## The three flows to explain

**1. Redirect (`GET /{code}`)**, in handlers_redirect.go
1. `codeShape`: cheap charset check.
2. Redis `GET k:l:{code}`. On a hit we're done, with no DB call.
3. Bloom filter. If it says "definitely not", return 404 with no DB call (stops random-code floods).
4. `SELECT` from the single shard `ring.Get(code)` picks, then fill Redis (cache-aside).
5. Reply 301/302 and, without waiting, do two things:
   - `store.BumpClicks`: in-memory counter, flushed to Postgres every 2s as one `UPDATE … FROM (VALUES …)` per shard.
   - `bus.Produce`: non-blocking channel send. If the buffer is full the event is dropped and counted; the redirect never waits.

**2. Shorten (`POST /api/links`)**, in handlers_links.go
Rate-limit → validate URL/title → `ids.Next()` → `encodeCode(id)` (or check the
alias: reserved? Bloom? shard?) → INSERT into the owning shard → write-through
to Redis → add to the Bloom filter. The response includes per-step timings.

**3. Click analytics**, in bus.go → olap.go
`dispatch` goroutine → partition `hash(code) % P` → one consumer per partition
polls ≤4000 events every 50ms → `OLAP.Insert` writes an immutable columnar part
*and* bumps per-code counters (minute/hour/day/dimension). Dashboard queries read
those counters (O(buckets)); `mode=raw` scans the columns (O(rows)), and
`/analytics/{code}/race` shows the two give identical totals.

## Likely interview questions

- **Why Snowflake and not auto-increment?** Unique across nodes without coordination, sorts by time, and creating one needs no DB round trip.
- **Why multiply before base62?** Sequential IDs would make codes guessable. Multiplying by an odd number mod 2⁶⁴ is reversible (we keep the modular inverse), so decoding still needs no lookup.
- **Clock goes backwards?** We keep using the last timestamp (`ClockBackwards` counter). If 4096 IDs are used in one ms, we wait for the next ms (`SeqExhausted`).
- **Why consistent hashing?** With `hash % N`, going from 4 to 5 shards moves ~80% of keys; with the ring it's ~20%. `/api/lab/ring` measures both.
- **Bloom false positives?** They only cost one extra shard lookup and are counted. False negatives can't happen.
- **Is analytics consistent?** Eventually. The bus is in-memory, so a crash loses unconsumed events. Kafka would be the production swap; Partition already has the same offset/commit contract.
- **Why HttpOnly cookies?** JavaScript can't read the tokens, so XSS can't steal them. Refresh tokens are single-use: Redis `SETNX` revokes the token and detects replays in one atomic step.
- **Redis down?** Rate limits fail open, cache misses fall through to Postgres, and refresh returns 503.
- **Login enumeration?** Unknown emails still run bcrypt against a dummy hash, so timing reveals nothing. There's also a per-IP+email lockout after 5 failures.

## Run the tests

```sh
go test -race ./...
```
