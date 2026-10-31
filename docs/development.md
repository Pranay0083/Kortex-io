# Running and Developing Kortex

## Start the application

```bash
cp .env.example .env
# Set JWT_SECRET (openssl rand -hex 32). POSTGRES_URL defaults to the bundled postgres service.
docker compose up --build
```

The service listens on `http://localhost:8090`. With `DEMO_MODE=true` the login page offers **Try the demo**, which signs visitors into a shared, non-admin demo account that owns the seeded links; those links cannot be deleted or disabled, and missing ones are re-created on boot. Sign in as the admin (`ADMIN_EMAIL` / `ADMIN_PASSWORD`) for the admin-only load generator. All configuration is documented in `.env.example`.

## Local development

```bash
# backend (needs Postgres + Redis, e.g. `docker compose up postgres redis`)
cd backend && go run .
go test -race ./...

# frontend (Vite dev server on :3000, proxies /api to :8090)
cd frontend && yarn install && yarn dev
yarn lint && yarn test && yarn build
```

Geography comes from Cloudflare's `CF-IPCountry` header or an optional MaxMind GeoLite2 database (`GEOIP_DB`); otherwise real clicks are recorded as `unknown`. Only seeded and ingest-benchmark events use synthetic geography. The console doesn't show country or city, since without one of those sources live clicks would all read "unknown".

Once running, the service automatically migrates the user and shard tables, seeds demo links, seeds 180,000 analytics events by default, warms Redis, starts broker consumers, and serves the compiled React application.

See the code tours for [backend internals](../backend/README.md) and the [frontend](../frontend/README.md).

## Demonstration flows

1. Register or sign in from the React console.
2. Create a link with a generated or custom alias.
3. Resolve the short URL repeatedly and observe the Redis `HIT` response header.
4. Open the overview to see clicks over time, referrers, devices and live clicks.
5. Open a link detail page and run the raw-versus-materialized-view query race.
6. Open the Infrastructure page for cache, event-log, columnar-store and shard statistics.
7. As the admin, open the Benchmark page and run redirect or ingestion tests (it is hidden from other users).

## Verified local benchmark snapshot

These measurements were collected against the Docker deployment. Results vary with host resources and are single-node measurements.

| Test | Result |
|---|---:|
| Redirect benchmark | 2,041,654 requests / 60 seconds |
| Average redirect throughput | 34,023 RPS |
| Peak redirect throughput | 66,683 RPS |
| Redirect p50 / p95 / p99 | 10.19 / 43.20 / 73.01 ms |
| Redis cache hit rate | 100% |
| Ingestion benchmark | 296,555 events/sec |
| Analytics MV speedup | 490.9x |
| Snowflake burst | 500,000 IDs, 0 collisions, 4.08M IDs/sec |
| 4-to-5 shard movement | 20.44% with consistent hashing vs 79.85% modulo |
| Bloom-filter measured FPR | 1.00% |

Benchmarks are admin-only and target 16 dedicated admin-owned links (`bench-01` to `bench-16`, a reserved alias prefix, re-created on boot), so they never touch customer or demo analytics. On boot the seeded demo links' stored click totals are reset to match the rebuilt demo history. The page keeps the last result of each benchmark type. The benchmark is a loopback, single-node measurement and is not a claim about a multi-node production cluster.
