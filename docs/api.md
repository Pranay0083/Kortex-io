# API / Interface Design

## Public and authentication endpoints

| Method | Endpoint | Purpose |
|---|---|---|
| `POST` | `/api/auth/register` | Create a user. |
| `POST` | `/api/auth/login` | Set access and refresh cookies; body is `{ user }`. |
| `POST` | `/api/auth/demo` | Demo mode only: sign in to the shared non-admin demo account; body is `{ user }`. |
| `POST` | `/api/auth/refresh` | Rotate the refresh token (single use; replays are rejected). |
| `POST` | `/api/auth/logout` | Revoke tokens and clear cookies (works without a valid session). |
| `GET` | `/api/system/stats` | Cache, broker, OLAP, shard, and runtime statistics (cached ~5s). |
| `GET` | `/api/bench/status` | Current benchmark snapshot. |
| `GET` | `/api/lab/*` | Snowflake, consistent-hash, and Bloom-filter playground endpoints (API only; inputs are clamped). |
| `GET` | `/api/health` | Check service and PostgreSQL health. |
| `GET` | `/api/r/{code}` | Resolve a short link and redirect. |
| `GET` | `/api/resolve/{code}` | Resolve a link as JSON (`status`, `active`, `expired`; no destination for disabled/expired links). |
| `GET` | `/api/qr/{code}` | Generate a 512px PNG QR code. |

## Authenticated link and analytics endpoints

| Method | Endpoint | Purpose |
|---|---|---|
| `POST` | `/api/links` | Create a link. |
| `GET` | `/api/links` | List links owned by the current user. |
| `GET` | `/api/links/check-alias?alias=x` | Validate alias availability. |
| `GET` | `/api/links/{code}` | Get link details. |
| `POST` | `/api/links/{code}/toggle` | Enable or disable a link. |
| `DELETE` | `/api/links/{code}` | Delete an owned link. |
| `GET` | `/api/analytics/overview?range=` | Account-level analytics for `1h`, `6h`, `24h`, `7d`, or `30d`. |
| `GET` | `/api/analytics/{code}/timeseries` | Time-series analytics. |
| `GET` | `/api/analytics/{code}/breakdown?dim=&range=` | Dimension breakdown over a time range. |
| `GET` | `/api/analytics/{code}/summary` | Aggregate summary. |
| `GET` | `/api/analytics/{code}/race` | Compare raw scan and materialized view. |
| `GET` | `/api/stream/recent` | Recent click events for the caller's links (admin sees all; no IPs). |
| `POST` | `/api/bench/start`, `/api/bench/stop` | Run or stop a benchmark (admin only). |

## Representative request/response

```http
POST /api/links
Content-Type: application/json
Cookie: kortex_access=<access-token>

{
  "url": "https://example.com/article",
  "alias": "article",
  "title": "Example article",
  "expires_in": "7d",
  "permanent": false
}
```

```json
{
  "link": { "code": "article", "long_url": "https://example.com/article", "redirect_type": 302, "shard": 2, "custom": true },
  "short_url": "http://localhost:8090/api/r/article",
  "pretty_url": "http://localhost:8090/article",
  "timings": { "id_gen_us": 1, "pg_write_us": 1432, "redis_us": 318, "shard": 2, "total_us": 1810 },
  "snowflake": { "node": 1, "sequence": 0, "code": "2xTQWRF80qO" }
}
```

API clients may send `Authorization: Bearer <access-token>` instead of the cookie.

The transport layer is implemented in `handlers_links.go`, `handlers_analytics.go`, `handlers_lab.go`, and `bench.go`; storage and domain behavior are implemented in `pg.go`, `cache.go`, `shard.go`, `bus.go`, `olap.go`, `bloom.go`, and `idgen.go`.
