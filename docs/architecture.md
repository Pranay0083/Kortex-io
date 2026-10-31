# Architecture

This document maps the implementation to the low-level system-design evaluation criteria.

## Architecture at a glance

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

The default deployment uses four logical PostgreSQL shards, four click-event partitions, Redis 7, PostgreSQL 15, a Go 1.24 backend, and a React 19 frontend. The system is packaged as a multi-stage Docker image and can be started with Docker Compose.

## 1. Requirement Analysis

### Actors

| Actor | Responsibilities |
|---|---|
| Anonymous visitor | Resolve a short code, receive a redirect, or request a QR code. |
| Registered user | Create, list, inspect, disable, delete, and analyze owned links. |
| Operator | Inspect health, cache, shard, broker, runtime, and OLAP statistics. |
| Benchmark client | Generate redirect load or synthetic click events and collect metrics. |
| Background workers | Flush click counters, consume event partitions, merge columnar parts, and maintain rollups. |

### Functional requirements

1. Register and authenticate users with access and refresh tokens.
2. Create short links from HTTP/HTTPS URLs with optional custom aliases, redirect types, titles, and expiry times.
3. Resolve a short code with low latency and return the configured redirect response.
4. Track click events without blocking the redirect response.
5. Expose link-level and account-level analytics by time, geography, referrer, device, browser, operating system, and bot status.
6. Support QR-code generation, link activation/deactivation, deletion, and alias availability checks.
7. Expose operational statistics and an interactive lab for Snowflake IDs, consistent hashing, and Bloom filters.
8. Provide built-in redirect and event-ingestion benchmarks.

### Non-functional requirements

- Low redirect latency: Redis should serve the normal read path; PostgreSQL is the fallback.
- High read throughput: redirects must not synchronously update PostgreSQL.
- Eventual analytics consistency: click counters and analytics may lag redirects by a bounded amount.
- Deterministic partitioning: link ownership must map to exactly one shard.
- Correctness: IDs and short-code mappings must be collision-free and reversible.
- Resilience: event-buffer pressure is bounded and observable; dependency startup is retried.
- Security: passwords are bcrypt-hashed; browser sessions use HttpOnly, SameSite=Lax cookies with single-use rotating refresh tokens; CORS is allowlisted; login and registration are rate limited per client IP; forwarded-for headers are trusted only from configured proxies.
- Operability: cache hit rate, broker lag, dropped events, query timings, runtime memory, and shard distribution are observable.

## 2. Class / Type Diagram

Go does not provide class inheritance. Kortex uses structs for state, methods for behavior, interfaces for polymorphism, and composition for relationships.

```mermaid
classDiagram
    class App {
      Config cfg
      Snowflake sf
      Ring ring
      Store store
      Cache cache
      Broker bus
      OLAP olap
      Bloom bloom
      Bench bench
    }
    class Config {
      string Port
      string PGURL
      string RedisURL
      int ShardCount
      int VNodes
      int Partitions
      int CacheTTL
      int SeedEvents
    }
    class User {
      int64 ID
      string Email
      string Name
      string Role
      time CreatedAt
    }
    class Link {
      int64 ID
      string Code
      string LongURL
      string Title
      int64 UserID
      bool Custom
      int Redirect
      time ExpiresAt
      int64 Clicks
      bool Active
      int Shard
    }
    class Snowflake {
      int64 node
      int64 lastMS
      int64 seq
      Next() int64
    }
    class Ring {
      int vnodes
      []int shards
      []uint32 points
      Get(key) int
    }
    class Store {
      pgxpool Pool pool
      Ring ring
      InsertLink()
      GetByCode()
      ListByUser()
      BumpClicks()
      flushClicks()
    }
    class Cache {
      redis Client rdb
      Get()
      Set()
      Warm()
      RateLimit()
    }
    class Broker {
      string Topic
      []Partition parts
      Produce(ClickEvent)
      Start()
      Flush()
    }
    class Partition {
      []ClickEvent log
      int64 baseOff
      int64 committed
      append()
      poll()
      commit()
    }
    class ClickEvent {
      int64 TS
      string Code
      string Country
      string City
      string Ref
      string Device
      string Browser
      string OS
      bool Bot
      int Status
      bool Hit
    }
    class OLAP {
      map parts
      Dict dict
      map materializedViews
      Insert(batch)
      TimeSeriesRaw()
      TimeSeriesMV()
      BreakdownRaw()
      BreakdownMV()
      Summary()
    }
    class Bloom {
      []uint64 bits
      uint64 m
      uint64 k
      Add(key)
      MayContain(key) bool
    }
    class Bench {
      BenchSnapshot snap
      Histogram hist
      Snapshot()
    }
    App *-- Config
    App *-- Snowflake
    App *-- Ring
    App *-- Store
    App *-- Cache
    App *-- Broker
    App *-- OLAP
    App *-- Bloom
    App *-- Bench
    Store --> Ring : routes keys
    Broker *-- Partition
    Broker --> ClickEvent : produces/consumes
    OLAP --> ClickEvent : materializes
    Store --> Link : persists
    Link --> User : belongs to
    Bloom --> Store : avoids existence queries
    Bench --> App : exercises paths
```

### Relationship and multiplicity summary

- One `App` composes one runtime `Config`, `Snowflake`, `Ring`, `Store`, `Cache`, `Broker`, `OLAP`, `Bloom`, and `Bench`.
- One `Broker` owns four or more `Partition` instances, configurable through `BUS_PARTITIONS`.
- One `User` owns zero or more `Link` records.
- Each `Link` is routed to exactly one logical PostgreSQL shard by `Ring.Get(code)`.
- One `ClickEvent` belongs to one short-code stream and is consumed by exactly one broker partition selected by hash.
- `OLAP` stores many click events in day partitions and maintains multiple aggregate maps.

## 3. Object-Oriented Design

### Abstraction

- `rnd` abstracts random-number generation so production request enrichment can use lock-free `math/rand/v2`, while deterministic paths use `math/rand.Rand`.
- HTTP handlers depend on domain operations such as `resolve`, `Store.GetByCode`, `Cache.Get`, and `OLAP.TimeSeriesMV` rather than embedding all storage logic in routes.
- `ClickEvent`, `Link`, `User`, `RingReport`, and benchmark snapshots are explicit domain structures.

### Encapsulation

- Ring internals (`points`, `owner`) are private and accessed through `Get`.
- Cache statistics and broker counters use atomic fields and controlled methods.
- Partition offsets are protected by a mutex and changed through `append`, `poll`, and `commit`.
- Password hashing, token parsing, authorization, and cookie handling are centralized in `auth.go`.

### Inheritance and polymorphism

Go deliberately avoids class inheritance. The equivalent design is interface satisfaction and composition:

- `*rand.Rand` and `fastRand` both satisfy the `rnd` interface.
- HTTP middleware composes CORS, recovery, authentication, and redirect counting.
- Raw-scan and materialized-view analytics implement the same conceptual query operation and are selected by the `mode` request parameter.

## 4. SOLID Principles

| Principle | Application in Kortex |
|---|---|
| Single Responsibility | `Cache` handles Redis, `Store` handles PostgreSQL, `Ring` handles routing, `Broker` handles event delivery, `OLAP` handles analytics, and handlers translate HTTP. |
| Open/Closed | New analytics dimensions can be added through OLAP dimension maps; new benchmark kinds can be added without changing the transport contract. |
| Liskov Substitution | Any implementation of `rnd` can be supplied to request enrichment without changing its behavior contract. |
| Interface Segregation | Small interfaces such as `rnd` avoid forcing callers to depend on unrelated methods. |
| Dependency Inversion | `App` wires dependencies at startup; handlers call domain components instead of managing SQL, Redis protocol, or partition internals. |

## 5. Design Patterns

| Pattern | Where it appears | Why it is used |
|---|---|---|
| Dependency Injection / Composition Root | `main.go` constructs `Config`, `Ring`, `Store`, `Cache`, `Broker`, `OLAP`, and `Bench`. | Centralizes wiring and makes dependencies explicit. |
| Strategy | `mode=raw` versus `mode=mv` analytics execution. | Allows the same endpoint to compare full scans with pre-aggregated queries. |
| Producer-Consumer | `Broker.Produce`, partition dispatch, and partition consumers. | Decouples redirect latency from analytics persistence. |
| Observer / Publish-Subscribe | Redirects publish `ClickEvent` values; consumers react asynchronously. | Allows analytics consumers to evolve independently of redirects. |
| Repository | `Store` methods such as `InsertLink`, `GetByCode`, `ListByUser`, and `DeleteLink`. | Encapsulates SQL and shard-table selection. |
| Cache-Aside | `resolve` checks Redis, falls back to PostgreSQL, then repopulates Redis. | Reduces database reads while retaining PostgreSQL as source of truth. |
| Object Pool | Redis and PostgreSQL connection pools. | Reuses connections and bounds resource consumption. |
| Builder-style Pipeline | `Cache.Warm` uses a Redis pipeline. | Reduces round trips during startup cache warming. |
| Adapter | HTTP handlers adapt JSON requests into domain calls and domain results into JSON. | Separates transport from business logic. |

Kortex does not use a Singleton or inheritance hierarchy because process-wide mutable singletons and class inheritance would make lifecycle, testing, and dependency ownership less explicit in Go.

## 6. Sequence and Activity Diagrams

### Short-link redirect sequence

```mermaid
sequenceDiagram
    actor Visitor
    participant API as Go Redirect Handler
    participant Redis
    participant Ring as Consistent Hash Ring
    participant PG as PostgreSQL Shard
    participant Bus as Click Broker
    participant OLAP as Columnar Analytics
    Visitor->>API: GET /api/r/{code}
    API->>Redis: GET link:{code}
    alt cache hit
        Redis-->>API: CachedLink
    else cache miss
        API->>Ring: shard = Get(code)
        Ring-->>API: shard id
        API->>PG: SELECT link from links_s{shard}
        PG-->>API: Link
        API->>Redis: SET link:{code}, TTL
    end
    API->>API: Validate active and expiry
    API->>API: Buffer click counter
    API-)Bus: Produce ClickEvent
    API-->>Visitor: 302/301 redirect
    Bus->>OLAP: Consume partition batch
    OLAP->>OLAP: Insert day part and update rollups
    Note over API,PG: Counter updates flush asynchronously every 2 seconds
```

### Link creation activity

```mermaid
flowchart TD
    A[POST /api/links] --> B{Authenticated?}
    B -- No --> E401[401 Unauthorized]
    B -- Yes --> C[Validate URL, alias, redirect type, expiry]
    C --> D{Valid?}
    D -- No --> E400[400 Validation error]
    D -- Yes --> E{Custom alias?}
    E -- Yes --> F[Bloom membership check]
    F --> G{Definitely absent?}
    G -- No --> H[Query owning PostgreSQL shard]
    G -- Yes --> I[Skip database existence query]
    H --> J{Available?}
    J -- No --> E409[409 Alias conflict]
    J -- Yes --> K[Generate Snowflake ID]
    I --> K
    E -- No --> K
    K --> L[Scramble ID and encode Base62]
    L --> M[Consistent-hash code to shard]
    M --> N[Insert link in PostgreSQL]
    N --> O[Write-through to Redis]
    O --> P[Return short URL and shard metadata]
```
