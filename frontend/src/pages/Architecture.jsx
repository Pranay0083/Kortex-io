import { Link } from "react-router-dom";
import { ArrowLeft, ArrowRight } from "lucide-react";
import { Chip, Panel } from "@/components/kx";

const Code = ({ children }) => (
  <pre className="kx-panel overflow-x-auto bg-black p-4 font-mono text-[11px] leading-relaxed text-zinc-400">
    {children}
  </pre>
);

const Section = ({ n, title, children }) => (
  <section className="grid gap-8 border-t border-kx-line py-12 lg:grid-cols-12">
    <div className="lg:col-span-4">
      <div className="kx-label">{n}</div>
      <h2 className="mt-3 font-display text-2xl font-bold tracking-tight lg:text-3xl">{title}</h2>
    </div>
    <div className="space-y-5 lg:col-span-8">{children}</div>
  </section>
);

const P = ({ children }) => <p className="text-[14px] leading-relaxed text-zinc-400">{children}</p>;

export default function Architecture() {
  return (
    <div className="min-h-screen bg-kx-bg">
      <header className="sticky top-0 z-50 border-b border-kx-line bg-black/85 backdrop-blur-xl">
        <div className="mx-auto flex max-w-[1200px] items-center justify-between px-5 py-3.5 lg:px-10">
          <Link to="/" className="flex items-center gap-2.5" data-testid="arch-home-link">
            <ArrowLeft size={14} className="text-zinc-500" />
            <div className="h-3 w-3 bg-kx-cyan" />
            <span className="font-display text-lg font-black tracking-tighter">KORTEX</span>
          </Link>
          <div className="flex gap-2">
            <Link
              to="/lab"
              className="kx-btn border border-kx-line px-4 py-2 text-zinc-300 hover:border-zinc-500"
            >
              Design lab
            </Link>
            <Link to="/app" className="kx-btn bg-kx-cyan px-4 py-2 text-black hover:bg-white">
              Console
            </Link>
          </div>
        </div>
      </header>

      <div className="mx-auto max-w-[1200px] px-5 py-14 lg:px-10 lg:py-20" data-testid="architecture-page">
        <div className="max-w-3xl">
          <div className="kx-label">Engineering notes</div>
          <h1 className="mt-3 font-display text-4xl font-black leading-[0.95] tracking-tighter lg:text-6xl">
            How Kortex is put together, and what was traded away.
          </h1>
          <div className="mt-6 flex flex-wrap gap-2">
            <Chip tone="cyan">Go 1.24</Chip>
            <Chip>PostgreSQL 15 · 4 shards</Chip>
            <Chip>Redis 7 · allkeys-lru</Chip>
            <Chip>partitioned event log</Chip>
            <Chip>columnar MergeTree store</Chip>
          </div>
        </div>

        <Section n="00 · topology" title="What actually runs in this container">
          <P>
            The API is a single Go binary on <code className="text-kx-cyan">:8090</code>. Postgres 15 and
            Redis 7 are real server processes managed by supervisor alongside it. An edge proxy on{" "}
            <code className="text-kx-cyan">:8001</code> forwards traffic in, because the platform owns
            that port — it adds one loopback hop and nothing else.
          </P>
          <P>
            Kafka and ClickHouse are the two pieces that cannot honestly run in a pod this size. Instead
            of stubbing them out, their contracts are implemented in-process: an append-only partitioned
            log with offsets, consumer groups and lag accounting; and a columnar store with immutable
            parts, day partitions, dictionary-encoded low-cardinality columns, background merges and
            incrementally maintained materialized views. Every code path you see exercised on the
            dashboard is executing for real — the substitution is the storage engine, not the design.
          </P>
          <Code>{`supervisor
├── kortex      go binary        :8090   api + redirect + analytics + loadgen
├── postgresql  postgres 15      :5432   users + links_s0..s3
├── redis       redis 7          :6379   read-path cache, rate limits, lockouts
└── backend     edge proxy       :8001   platform-owned ingress -> :8090`}</Code>
        </Section>

        <Section n="01 · write path" title="Snowflake → base62 → shard → warm cache">
          <Code>{`POST /api/links
  ├─ rate limit        redis INCR, 40 links/min/tenant
  ├─ id = snowflake    (ms << 22) | (node << 12) | seq
  ├─ code = base62(id * MULT mod 2^58)      10 chars, non-enumerable
  ├─ alias?            bloom filter -> maybe? -> shard SELECT
  ├─ shard = ring.get(code)                 consistent hash, 160 vnodes
  ├─ INSERT INTO links_s{n}
  └─ SET k:l:{code} (write-through, 1h TTL)`}</Code>
          <P>
            The generator hands out 4096 IDs per node per millisecond with no network round trip, which
            is the entire point: a database sequence would put a coordination step on the hot write path
            and make the shortener only as available as its ID authority. The 10-bit node field is the
            only thing that needs to be assigned externally, once, at deploy time.
          </P>
          <P>
            Raw Snowflake IDs are monotonic and therefore enumerable, so before encoding we multiply by a
            fixed odd constant modulo 2<sup>58</sup>. That is a bijection, so it is perfectly reversible
            with the modular inverse — decoding a code needs no lookup — but adjacent IDs land in
            completely different regions of code space.
          </P>
        </Section>

        <Section n="02 · sharding" title="Hash prefix, ring, and the resize question">
          <P>
            Links are sharded by the code, not the user, because the read path only ever knows the code.
            A consistent hash ring with 160 virtual nodes per shard maps codes to{" "}
            <code className="text-kx-cyan">links_s0..s3</code>. Today those are four tables in one
            cluster; the ring is what lets them become four physical instances without rewriting a single
            key mapping.
          </P>
          <P>
            Naive <code>hash(code) % N</code> is fine until you resize: going from four shards to five
            remaps roughly 80% of all keys, which means a full rewrite and a completely cold cache.
            The ring moves only the slice the new shard claims — about 20% — and virtual nodes keep that
            slice evenly spread instead of dumping one neighbour's entire range onto the newcomer. The{" "}
            <Link to="/lab" className="text-kx-cyan hover:underline">
              design lab
            </Link>{" "}
            lets you resample this live.
          </P>
          <P>
            The cost is scatter-gather: listing a tenant's links has to query every shard and merge.
            That is acceptable because it is a dashboard query, not a redirect. Redirects always know
            their shard from the code alone.
          </P>
        </Section>

        <Section n="03 · read path" title="Redis first, Postgres never if we can help it">
          <Code>{`GET /api/r/{code}
  ├─ GET k:l:{code}          hit -> 99%+ of traffic
  ├─ miss -> SELECT FROM links_s{ring(code)}
  │          └─ write back into redis
  ├─ expiry / disabled checks in-memory
  ├─ 301 or 302 + Location
  └─ produce ClickEvent -> buffered channel (never awaited)`}</Code>
          <P>
            Nothing on the redirect path writes to Postgres synchronously. Click counters accumulate in a
            map and get flushed every two seconds as a single multi-row UPDATE per shard; one UPDATE per
            redirect would cap throughput at Postgres write speed and turn a read workload into a write
            workload.
          </P>
          <P>
            The event emit uses a buffered channel with a <code>select/default</code>: if the buffer is
            genuinely full the event is dropped and counted rather than blocking a user-facing redirect.
            Analytics being seconds stale is fine. A redirect being 40ms slow is not.
          </P>
          <P>
            The cache is warmed at boot with the hottest codes and runs <code>allkeys-lru</code>, so the
            long cold tail is what gets evicted under pressure and the working set stays resident. Hit
            rate, evictions and resident keys are all on the{" "}
            <Link to="/app/ops" className="text-kx-cyan hover:underline">
              infra panel
            </Link>
            .
          </P>
        </Section>

        <Section n="04 · analytics" title="Log → consumer group → columnar parts → rollups">
          <Code>{`clicks topic, 4 partitions, key = hash(code)
  producer   non-blocking, drop-counted
  consumer   poll -> batch <= 20k -> insert -> commit offset
  storage    one immutable part per insert, partitioned by day
             dictionary-encoded columns (country, ref, device, browser, os)
             background merge folds small parts into level-1 parts
  views      per-minute / per-hour / per-day counts
             + per-country / city / referrer / device / browser / os`}</Code>
          <P>
            Rollups are maintained on write, not on read. Every inserted row bumps nine counters. That
            makes ingestion slightly more expensive and makes every dashboard query a map lookup over a
            few hundred buckets instead of a scan over millions of values.
          </P>
          <P>
            The trade-off is real and worth naming: materialized views only answer the questions you
            defined in advance. Anything ad-hoc still needs the raw columns. So both paths stay
            available, and the <span className="text-white">Race</span> button on any link runs them
            side by side over whatever is currently in the store — typically a 25–90× gap at a few
            hundred thousand rows, widening as the table grows.
          </P>
        </Section>

        <Section n="05 · limits" title="Where this would break, and what comes next">
          <P>
            <span className="text-white">Single node.</span> One process, one ring, one columnar store in
            memory. The obvious next step is moving shards onto separate Postgres instances and swapping
            the in-process log for a real Kafka cluster — the interfaces are already shaped for it.
          </P>
          <P>
            <span className="text-white">Durability.</span> The columnar store is memory-resident with no
            WAL. Postgres remains the source of truth for links; click history would not survive a
            restart. A real deployment writes parts to disk and replays the log from the last committed
            offset.
          </P>
          <P>
            <span className="text-white">Geo data.</span> There is no MaxMind database in this container,
            so country and city are synthesised from a weighted distribution for load traffic. The
            pipeline, the dictionary encoding and the rollups are all real — the enrichment step is the
            one place that is stubbed.
          </P>
          <P>
            <span className="text-white">Benchmark honesty.</span> The load generator runs inside the
            same process it is testing, on shared throttled cores, behind an extra proxy hop. Numbers
            here will land well below a dedicated node. That is stated on the benchmark page rather than
            hidden behind a nicer figure.
          </P>
        </Section>

        <div className="mt-8 flex flex-wrap gap-3 border-t border-kx-line pt-10">
          <Link
            to="/lab"
            data-testid="arch-to-lab"
            className="kx-btn bg-kx-cyan px-6 py-3 text-black hover:bg-white"
          >
            Poke at the design lab <ArrowRight size={13} />
          </Link>
          <Link
            to="/app/bench"
            data-testid="arch-to-bench"
            className="kx-btn border border-kx-line px-6 py-3 text-white hover:border-zinc-500"
          >
            Run the benchmark
          </Link>
        </div>
      </div>
    </div>
  );
}
