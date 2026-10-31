# Kortex

Kortex is a distributed URL-shortening and click-analytics platform. It combines a Redis-first redirect path, PostgreSQL link storage, consistent-hash sharding, asynchronous click-event processing, a columnar analytics engine, and a React operations console.

```text
React console / browser
          |
          v
   Go HTTP API + router
      |          |
      |          +--> Redis: link cache, rate limits, login lockouts
      |
      +--> Consistent-hash ring --> PostgreSQL links_s0 ... links_s3
      |
      +--> Non-blocking click broker --> partition consumers
                                      |
                                      v
                         columnar day partitions + rollups
```

The default deployment uses four logical PostgreSQL shards, four click-event partitions, Redis 7, PostgreSQL 15, a Go 1.24 backend, and a React 19 frontend, packaged as a multi-stage Docker image.

## Quick start

```bash
cp .env.example .env
# Set JWT_SECRET (openssl rand -hex 32). POSTGRES_URL defaults to the bundled postgres service.
docker compose up --build
```

The service listens on `http://localhost:8090`. See [docs/development.md](docs/development.md) for local (non-Docker) development, demo flows, and benchmark results.

## Technology stack

- Backend: Go 1.24, chi, pgx, Redis client, JWT, bcrypt
- Frontend: React 19, Vite, React Router, Recharts, Tailwind CSS, Vitest + Testing Library, ESLint 9
- Data: PostgreSQL 15, Redis 7, in-process partitioned event log, columnar OLAP store
- Deployment: Docker multi-stage build (non-root, healthchecked), Docker Compose (Go service, PostgreSQL 15, Redis 7), GitHub Actions CI (gofmt, vet, race tests, lint, frontend tests, image publish)
- Algorithms: Snowflake IDs, Base62, bijective scrambling, consistent hashing, Bloom filters, materialized views

## Documentation

Full system-design documentation lives in [`docs/`](docs/README.md):

- [Architecture](docs/architecture.md) — requirements, class diagram, OOD, SOLID, design patterns, sequence/activity diagrams
- [API](docs/api.md) — HTTP endpoints and a representative request/response
- [Database](docs/database.md) — schema and sharding
- [Reliability](docs/reliability.md) — validation, error handling, concurrency, backpressure
- [Development](docs/development.md) — running locally, demo flows, benchmark results

For a tour of the code itself: [backend/README.md](backend/README.md), [frontend/README.md](frontend/README.md).
