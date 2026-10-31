# Validation, Error Handling, and Edge Cases

## Input validation

- URLs are normalized to `https://` when no scheme is provided; only `http`/`https` URLs up to 2048 characters are accepted, and links back to the service's own host are rejected to prevent redirect loops.
- Custom aliases must be 3–32 characters and contain only letters, digits, `-`, or `_`.
- Reserved aliases such as `api`, `app`, `login`, `register`, `lab`, and `architecture` are rejected so they never shadow console routes.
- JSON bodies are capped at 64KB; titles at 200 characters; list limits at 500.
- Benchmark duration, concurrency, and event counts are clamped; lab endpoint inputs are clamped so public endpoints cannot allocate unbounded memory.
- Generated codes are 11 base62 characters: the 64-bit Snowflake ID multiplied by an odd constant modulo 2^64, a true bijection that decodes without a database lookup.

## Runtime failures

- Missing or invalid JWTs return `401 Unauthorized`.
- Missing links return `404` (the global Bloom filter answers most misses without touching PostgreSQL, and unknown codes never enter the analytics store); disabled or expired links return `410`; database failures return `503`, not `404`.
- Browser requests for unknown codes receive the SPA with status `404` so the console renders a not-found page.
- Duplicate aliases return a conflict response after shard-level existence checking.
- PostgreSQL and Redis startup retries tolerate dependencies becoming ready after the application process.
- Redis misses fall through to PostgreSQL and repopulate the cache.
- Redis errors are counted in cache statistics.
- The HTTP router uses panic recovery middleware.
- SIGINT/SIGTERM trigger a graceful shutdown: in-flight requests finish, the broker drains, and pending click counters are flushed. Failed click flushes are retried on the next tick.
- The server refuses to start with an empty, short (<32 chars), or placeholder `JWT_SECRET`.

## Concurrency and backpressure

- Snowflake generation is mutex-protected and records clock-backward and sequence-exhaustion counters.
- Cache, Bloom-filter, partition, dictionary, and OLAP state use mutexes or atomics where required; the test suite runs under `go test -race`.
- Partition logs trim in amortized halves once they exceed twice the retention, and events trimmed before consumption are counted.
- Analytics older than `ANALYTICS_RETENTION_DAYS` (default 30) are dropped by the background merger; minute rollups are kept for 48 hours.
- Deleting a link purges its analytics, so a reused alias never inherits another owner's history.
- The broker ingress buffer is bounded. If full, a click event is dropped and `Dropped` is incremented instead of blocking redirects.
- Consumers process bounded batches and commit partition offsets after sink writes.
- Click counters are aggregated in memory and flushed to PostgreSQL in shard-grouped batches every two seconds.
- Analytics queries can choose raw or pre-aggregated execution, making correctness and performance directly comparable.
