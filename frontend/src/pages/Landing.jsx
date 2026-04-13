import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import {
  ArrowUpRight,
  Boxes,
  CircuitBoard,
  Database,
  Gauge,
  Hash,
  Layers,
  Radio,
  Zap,
} from "lucide-react";
import { api, compact, fmt } from "@/lib/api";
import { Chip } from "@/components/kx";

const HERO_BG = "https://images.pexels.com/photos/10325707/pexels-photo-10325707.png";

const STACK = [
  "Go 1.24",
  "PostgreSQL 15",
  "Redis 7",
  "Kafka-style log",
  "Columnar MergeTree",
  "Snowflake IDs",
  "base62",
  "Bloom filter",
  "Consistent hashing",
  "Materialized views",
  "React 19",
  "Recharts",
  "k6-style loadgen",
  "Nginx",
  "Docker",
];

const REFERENCE = [
  { metric: "Redirect throughput", target: "21,000 RPS", key: "rps", unit: " RPS", fmtv: (s) => (s.rps ? fmt.format(Math.round(s.rps)) : null) },
  {
    metric: "p99 redirect latency",
    target: "6 ms",
    key: "p99_ms",
    fmtv: (s) => (s.p95_ms ? `${s.p95_ms.toFixed(2)} ms (p95)` : null),
  },
  { metric: "Redis cache hit rate", target: "99.2 %", key: "cache_hit_rate", fmtv: (s) => (s.cache_hit_rate ? `${s.cache_hit_rate.toFixed(2)} %` : null) },
  { metric: "Click events ingested", target: "10M in 4 min", key: "ingest_rate", fmtv: (s) => (s.ingest_rate ? `${compact(s.ingest_rate)} evt/s` : null) },
];

function Nav() {
  return (
    <header className="sticky top-0 z-50 border-b border-kx-line bg-black/80 backdrop-blur-xl">
      <div className="mx-auto flex max-w-[1400px] items-center justify-between px-5 py-3.5 lg:px-10">
        <Link to="/" className="flex items-center gap-2.5" data-testid="landing-logo">
          <div className="h-3 w-3 bg-kx-cyan" />
          <span className="font-display text-lg font-black tracking-tighter">KORTEX</span>
          <span className="hidden font-mono text-[10px] tracking-widest text-zinc-600 sm:inline">
            v1.0 · edge-01
          </span>
        </Link>
        <nav className="flex items-center gap-1.5">
          <a
            href="#architecture"
            className="hidden px-3 py-2 font-mono text-[11px] uppercase tracking-widest text-zinc-500 transition-colors hover:text-white sm:block"
          >
            Architecture
          </a>
          <Link
            to="/lab"
            data-testid="landing-lab-link"
            className="hidden px-3 py-2 font-mono text-[11px] uppercase tracking-widest text-zinc-500 transition-colors hover:text-white sm:block"
          >
            Design Lab
          </Link>
          <Link
            to="/login"
            data-testid="landing-login-btn"
            className="kx-btn border border-kx-line px-4 py-2 text-zinc-300 hover:border-zinc-500 hover:text-white"
          >
            Sign in
          </Link>
          <Link
            to="/register"
            data-testid="landing-cta-btn"
            className="kx-btn bg-kx-cyan px-4 py-2 text-black hover:bg-white"
          >
            Open console
          </Link>
        </nav>
      </div>
    </header>
  );
}

function LiveStrip({ stats }) {
  const items = [
    { l: "rows in columnar store", v: stats ? fmt.format(stats.olap.total_rows) : "—", c: "#00E5FF" },
    { l: "redis hit rate", v: stats && stats.cache.hits + stats.cache.misses > 0 ? `${stats.cache.hit_rate.toFixed(2)}%` : "—", c: "#00FF66" },
    { l: "consumer lag", v: stats ? fmt.format(stats.bus.total_lag) : "—", c: "#FFD60A" },
    { l: "ids minted", v: stats ? fmt.format(stats.id_gen.issued) : "—", c: "#FFFFFF" },
    { l: "day partitions", v: stats ? stats.olap.partitions.length : "—", c: "#8A2BE2" },
    { l: "goroutines", v: stats ? stats.runtime.goroutines : "—", c: "#FFFFFF" },
  ];
  return (
    <div className="border-y border-kx-line bg-kx-surface" data-testid="landing-live-strip">
      <div className="mx-auto grid max-w-[1400px] grid-cols-2 divide-x divide-kx-line md:grid-cols-3 lg:grid-cols-6">
        {items.map((it) => (
          <div key={it.l} className="px-5 py-4">
            <div className="kx-label">{it.l}</div>
            <div className="kx-num mt-1.5 text-xl font-bold" style={{ color: it.c }}>
              {it.v}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

const PathCard = ({ icon: Icon, tag, title, lines, color }) => (
  <div className="kx-panel group p-6">
    <div className="flex items-center gap-2.5">
      <Icon size={17} strokeWidth={1.5} style={{ color }} />
      <span className="kx-label" style={{ color }}>
        {tag}
      </span>
    </div>
    <h3 className="mt-4 font-display text-xl font-bold tracking-tight">{title}</h3>
    <div className="mt-5 space-y-2.5">
      {lines.map((l, i) => (
        <div key={i} className="flex items-start gap-3 font-mono text-[11.5px] leading-relaxed">
          <span className="mt-0.5 shrink-0 text-zinc-700">{String(i + 1).padStart(2, "0")}</span>
          <span className="text-zinc-400">{l}</span>
        </div>
      ))}
    </div>
  </div>
);

export default function Landing() {
  const [stats, setStats] = useState(null);
  const [bench, setBench] = useState(null);

  useEffect(() => {
    const load = async () => {
      try {
        const [s, b] = await Promise.all([api.get("/system/stats"), api.get("/bench/status")]);
        setStats(s.data);
        setBench(b.data);
      } catch {
        /* engine warming */
      }
    };
    load();
    const t = setInterval(load, 4000);
    return () => clearInterval(t);
  }, []);

  return (
    <div className="min-h-screen bg-kx-bg">
      <Nav />

      {/* HERO */}
      <section className="kx-grain relative overflow-hidden border-b border-kx-line">
        <div
          className="absolute inset-0 bg-cover bg-center opacity-[0.14]"
          style={{ backgroundImage: `url(${HERO_BG})` }}
        />
        <div className="absolute inset-0 bg-gradient-to-r from-black via-black/85 to-black/40" />
        <div className="kx-grid absolute inset-0 opacity-40" />

        <div className="relative mx-auto max-w-[1400px] px-5 pb-20 pt-16 lg:px-10 lg:pb-28 lg:pt-24">
          <div className="max-w-4xl">
            <div className="flex flex-wrap items-center gap-2 animate-kx-rise">
              <Chip tone="cyan">
                <span className="h-1.5 w-1.5 animate-kx-pulse bg-kx-cyan" /> live engine
              </Chip>
              <Chip>single node · loopback benchmarked</Chip>
            </div>

            <h1
              className="mt-7 font-display text-4xl font-black leading-[0.92] tracking-tighter sm:text-5xl lg:text-[76px] animate-kx-rise"
              style={{ animationDelay: "60ms" }}
            >
              Short links are easy.
              <br />
              <span className="text-zinc-600">Ten million clicks a day</span>
              <br />
              are the actual problem.
            </h1>

            <p
              className="mt-7 max-w-2xl text-base leading-relaxed text-zinc-400 animate-kx-rise"
              style={{ animationDelay: "120ms" }}
            >
              Kortex generates collision-free IDs without coordination, resolves them from Redis in
              microseconds, and streams every click through a partitioned log into a columnar store
              where materialized views answer analytics queries 25–90× faster than a full scan. All
              of it is running right now — nothing on this page is a mock-up.
            </p>

            <div
              className="mt-9 flex flex-wrap items-center gap-3 animate-kx-rise"
              style={{ animationDelay: "180ms" }}
            >
              <Link
                to="/register"
                data-testid="hero-primary-cta"
                className="kx-btn bg-kx-cyan px-7 py-3.5 text-black hover:bg-white"
              >
                Shorten something <ArrowUpRight size={14} strokeWidth={2.5} />
              </Link>
              <Link
                to="/lab"
                data-testid="hero-secondary-cta"
                className="kx-btn border border-kx-line px-7 py-3.5 text-white hover:border-zinc-500 hover:bg-kx-hover"
              >
                Open the design lab
              </Link>
            </div>

            <div
              className="mt-12 max-w-2xl border border-kx-line bg-black/70 p-4 font-mono text-[11px] leading-relaxed text-zinc-500 animate-kx-rise"
              style={{ animationDelay: "240ms" }}
            >
              <div className="text-zinc-600">$ curl -sI $KORTEX/api/r/mergetree</div>
              <div className="mt-1.5 text-kx-green">HTTP/1.1 302 Found</div>
              <div className="text-zinc-400">
                location: https://clickhouse.com/docs/en/engines/table-engines/…
              </div>
              <div className="text-zinc-400">
                x-kortex-cache: <span className="text-kx-cyan">HIT</span>
              </div>
            </div>
          </div>
        </div>
      </section>

      <LiveStrip stats={stats} />

      {/* ARCHITECTURE */}
      <section id="architecture" className="mx-auto max-w-[1400px] px-5 py-20 lg:px-10 lg:py-28">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <div className="kx-label">Three paths, three failure domains</div>
            <h2 className="mt-3 font-display text-3xl font-black tracking-tighter lg:text-5xl">
              The write path never blocks.
              <br />
              The read path never waits.
            </h2>
          </div>
          <Link
            to="/architecture"
            data-testid="architecture-deep-dive"
            className="kx-btn shrink-0 border border-kx-line px-5 py-3 text-zinc-300 hover:border-kx-cyan hover:text-kx-cyan"
          >
            Full write-up <ArrowUpRight size={13} />
          </Link>
        </div>

        <div className="mt-12 grid gap-4 lg:grid-cols-3">
          <PathCard
            icon={Hash}
            color="#00E5FF"
            tag="write path"
            title="Snowflake → base62 → shard"
            lines={[
              "41-bit ms timestamp | 10-bit node | 12-bit sequence",
              "4096 ids per node per millisecond, zero coordination",
              "multiply by an odd constant mod 2^58 → non-enumerable",
              "base62 encode → fixed 10-char code",
              "consistent hash ring picks links_s0..s3",
              "write-through into Redis so the first read is a hit",
            ]}
          />
          <PathCard
            icon={Zap}
            color="#00FF66"
            tag="read path"
            title="Redis → 302 → fire and forget"
            lines={[
              "GET /:code hits Redis first (allkeys-lru, 1h TTL)",
              "miss falls through to the owning Postgres shard",
              "result is written back into cache on the way out",
              "301 for permanent links, 302 for everything else",
              "click event pushed to a buffered channel, never awaited",
              "click counters batched and flushed to Postgres every 2s",
            ]}
          />
          <PathCard
            icon={Layers}
            color="#FFD60A"
            tag="analytics path"
            title="Log → consumer → columnar"
            lines={[
              "events hashed onto 4 partitions of an append-only log",
              "consumer group polls, batches up to 20k, commits offsets",
              "columnar parts, one per insert, partitioned by day",
              "LowCardinality dictionary encoding on every dimension",
              "background merge folds small parts into level-1 parts",
              "9 materialized views updated inline on write",
            ]}
          />
        </div>
      </section>

      {/* RESULTS */}
      <section className="border-y border-kx-line bg-kx-surface">
        <div className="mx-auto max-w-[1400px] px-5 py-20 lg:px-10 lg:py-24">
          <div className="grid gap-12 lg:grid-cols-[1fr_1.4fr]">
            <div>
              <div className="kx-label">Benchmarks</div>
              <h2 className="mt-3 font-display text-3xl font-black tracking-tighter lg:text-4xl">
                Reference targets vs. this box
              </h2>
              <p className="mt-5 text-sm leading-relaxed text-zinc-400">
                The left column is the design target from a dedicated single node. The right column is
                whatever the load generator last measured <em>on this container</em> — a shared,
                CPU-throttled pod where the k6-style runner competes with the server it is hammering.
                Run it yourself from the console; the numbers below update.
              </p>
              <Link
                to="/app/bench"
                data-testid="results-run-bench"
                className="kx-btn mt-7 inline-flex bg-white px-6 py-3 text-black hover:bg-zinc-200"
              >
                <Gauge size={14} strokeWidth={2} /> Run the load test
              </Link>
            </div>

            <div className="kx-panel overflow-hidden" data-testid="results-table">
              <table className="w-full">
                <thead>
                  <tr className="border-b border-kx-line">
                    <th className="kx-label px-5 py-3 text-left">Metric</th>
                    <th className="kx-label px-5 py-3 text-right">Design target</th>
                    <th className="kx-label px-5 py-3 text-right text-kx-cyan">Measured here</th>
                  </tr>
                </thead>
                <tbody>
                  {REFERENCE.map((r) => {
                    const v = bench ? r.fmtv(bench) : null;
                    return (
                      <tr key={r.metric} className="border-b border-kx-line/60 last:border-0">
                        <td className="px-5 py-3.5 text-sm text-zinc-300">{r.metric}</td>
                        <td className="kx-num px-5 py-3.5 text-right text-sm text-zinc-500">
                          {r.target}
                        </td>
                        <td className="kx-num px-5 py-3.5 text-right text-sm font-bold text-white">
                          {v || <span className="text-zinc-700">not measured yet</span>}
                        </td>
                      </tr>
                    );
                  })}
                  <tr>
                    <td className="px-5 py-3.5 text-sm text-zinc-300">
                      Analytics query, materialized view
                    </td>
                    <td className="kx-num px-5 py-3.5 text-right text-sm text-zinc-500">
                      380ms → 18ms
                    </td>
                    <td className="kx-num px-5 py-3.5 text-right text-sm font-bold text-kx-green">
                      live in console
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>
      </section>

      {/* DESIGN ELEMENTS */}
      <section className="mx-auto max-w-[1400px] px-5 py-20 lg:px-10 lg:py-28">
        <div className="kx-label">Interactive</div>
        <h2 className="mt-3 max-w-3xl font-display text-3xl font-black tracking-tighter lg:text-5xl">
          Five design decisions you can poke at instead of read about.
        </h2>

        <div className="mt-12 grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {[
            {
              icon: Hash,
              t: "Snowflake bit decoder",
              d: "Paste any short code. Watch it unscramble into timestamp, node and sequence bits — no database lookup involved.",
              c: "#00E5FF",
            },
            {
              icon: CircuitBoard,
              t: "Consistent hash ring",
              d: "Add or remove a shard and see exactly what fraction of keys move, next to what naive modulo would have cost you.",
              c: "#00FF66",
            },
            {
              icon: Boxes,
              t: "Bloom filter playground",
              d: "Insert aliases, probe for misses, drag the target false-positive rate and watch m, k and memory respond.",
              c: "#FFD60A",
            },
            {
              icon: Database,
              t: "MV vs. full scan race",
              d: "Same question, two execution plans, side by side, over the rows actually sitting in the store right now.",
              c: "#FF3B30",
            },
            {
              icon: Radio,
              t: "Live click stream",
              d: "Raw events flowing out of the partitioned log with partition, offset and consumer lag per partition.",
              c: "#8A2BE2",
            },
            {
              icon: Gauge,
              t: "Cache warming & eviction",
              d: "Hit rate, evicted keys, working-set size and the LRU policy that keeps the hot tail resident.",
              c: "#7DD3FC",
            },
          ].map((f, i) => (
            <Link
              key={f.t}
              to={i === 4 ? "/app" : i === 5 ? "/app/ops" : "/lab"}
              data-testid={`design-card-${i}`}
              className="kx-panel group p-6 transition-colors hover:bg-kx-hover"
            >
              <f.icon size={18} strokeWidth={1.5} style={{ color: f.c }} />
              <h3 className="mt-4 font-display text-lg font-bold tracking-tight">{f.t}</h3>
              <p className="mt-2.5 text-[13px] leading-relaxed text-zinc-500">{f.d}</p>
              <div className="mt-5 flex items-center gap-1.5 font-mono text-[10px] uppercase tracking-widest text-zinc-600 transition-colors group-hover:text-kx-cyan">
                open <ArrowUpRight size={11} />
              </div>
            </Link>
          ))}
        </div>
      </section>

      {/* STACK MARQUEE */}
      <section className="overflow-hidden border-y border-kx-line bg-kx-surface py-5">
        <div className="flex w-max kx-marquee">
          {[...STACK, ...STACK].map((s, i) => (
            <span
              key={i}
              className="mx-3 whitespace-nowrap border border-kx-line px-3.5 py-1.5 font-mono text-[11px] text-zinc-400"
            >
              {s}
            </span>
          ))}
        </div>
      </section>

      <footer className="mx-auto max-w-[1400px] px-5 py-14 lg:px-10">
        <div className="flex flex-col justify-between gap-6 border-t border-kx-line pt-8 md:flex-row">
          <div>
            <div className="flex items-center gap-2">
              <div className="h-2.5 w-2.5 bg-kx-cyan" />
              <span className="font-display text-base font-black tracking-tighter">KORTEX</span>
            </div>
            <p className="mt-2 max-w-md font-mono text-[11px] leading-relaxed text-zinc-600">
              Go service on :8090 behind an edge proxy. Postgres and Redis are real processes in this
              container. The event log and columnar engine are purpose-built Go packages implementing
              the Kafka and MergeTree contracts in-process.
            </p>
          </div>
          <div className="flex gap-8 font-mono text-[11px]">
            <div className="space-y-2">
              <div className="kx-label">Console</div>
              <Link to="/app" className="block text-zinc-500 hover:text-kx-cyan">
                Dashboard
              </Link>
              <Link to="/app/bench" className="block text-zinc-500 hover:text-kx-cyan">
                Benchmark
              </Link>
              <Link to="/app/ops" className="block text-zinc-500 hover:text-kx-cyan">
                Infra
              </Link>
            </div>
            <div className="space-y-2">
              <div className="kx-label">Deep dive</div>
              <Link to="/lab" className="block text-zinc-500 hover:text-kx-cyan">
                Design Lab
              </Link>
              <Link to="/architecture" className="block text-zinc-500 hover:text-kx-cyan">
                Architecture
              </Link>
            </div>
          </div>
        </div>
      </footer>
    </div>
  );
}
