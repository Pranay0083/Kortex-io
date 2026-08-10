import { useCallback, useState } from "react";
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { api, apiError, compact, fmt } from "@/lib/api";
import { useChartColors } from "@/context/ThemeContext";
import { usePolling } from "@/hooks/usePolling";
import { primeSystemInfo, systemInfoFrom } from "@/hooks/useSystemInfo";
import { Panel, Chip, ChartTip, ErrorNote, Skeleton, PageHeader } from "@/components/kx";

const PARTITIONS_COLLAPSED = 7;

const plural = (n, one, many = `${one}s`) => `${n === 1 ? one : many}`;
const count = (n, one, many) => `${fmt.format(n)} ${plural(n, one, many)}`;

/** 125 → "2m", 7300 → "2h 1m", 200000 → "2d 7h". */
function uptime(s) {
  const sec = Math.max(0, Math.floor(s || 0));
  if (sec < 60) return `${sec}s`;
  const m = Math.floor(sec / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ${m % 60}m`;
  return `${Math.floor(h / 24)}d ${h % 24}h`;
}

/** Thin accent ring for a percentage; "—" until there is anything to measure. */
function Ring({ value, empty, label, sub }) {
  const c = useChartColors();
  const pct = Math.max(0, Math.min(100, value || 0));
  const r = 40;
  const len = 2 * Math.PI * r;
  return (
    <div className="flex items-center gap-5">
      <svg
        width="96"
        height="96"
        viewBox="0 0 96 96"
        className="-rotate-90 shrink-0"
        aria-hidden="true"
      >
        <circle cx="48" cy="48" r={r} fill="none" stroke={c.track} strokeWidth="6" />
        {!empty && (
          <circle
            cx="48"
            cy="48"
            r={r}
            fill="none"
            stroke={c.accent}
            strokeWidth="6"
            strokeLinecap={pct > 0 ? "round" : "butt"}
            strokeDasharray={len}
            strokeDashoffset={len - (pct / 100) * len}
            style={{ transition: "stroke-dashoffset 800ms cubic-bezier(0.16,1,0.3,1)" }}
          />
        )}
      </svg>
      <div className="min-w-0">
        <div className="kx-label">{label}</div>
        <div className="mt-0.5 flex items-baseline gap-1">
          <span
            className={`kx-num text-3xl font-semibold ${empty ? "text-kx-muted" : "text-kx-ink"}`}
          >
            {empty ? "—" : pct.toFixed(1)}
          </span>
          {!empty && <span className="text-base text-kx-muted">%</span>}
        </div>
        <div className="kx-num mt-1 text-xs text-kx-muted">{sub}</div>
      </div>
    </div>
  );
}

/** Small figure inside a card: label over value, on the inset well. */
const Figure = ({ label, value, unit, tone = "text-kx-ink" }) => (
  <div className="rounded-control bg-kx-card px-3 py-2.5">
    <div className="kx-label">{label}</div>
    <div className={`kx-num mt-0.5 text-lg font-semibold ${tone}`}>
      {value}
      {unit && <span className="ml-1 text-xs font-normal text-kx-muted">{unit}</span>}
    </div>
  </div>
);

/** Label left (muted), value right (ink, tabular). */
const Row = ({ k, v }) => (
  <div className="flex items-baseline justify-between gap-3 border-b border-kx-line py-2 text-sm last:border-b-0">
    <span className="text-kx-muted">{k}</span>
    <span className="kx-num text-kx-ink">{v}</span>
  </div>
);

const th = "kx-label py-2 font-medium";

export default function Ops() {
  const [s, setS] = useState(null);
  const [err, setErr] = useState("");
  const [allParts, setAllParts] = useState(false);
  const c = useChartColors();

  const load = useCallback(async () => {
    try {
      const { data } = await api.get("/system/stats");
      setS(data);
      primeSystemInfo(data);
      setErr("");
    } catch (e) {
      setErr(apiError(e, "Could not load infrastructure stats"));
    }
  }, []);

  usePolling(load, 3000);

  if (!s)
    return (
      <div data-testid="ops-page" className="space-y-5">
        <PageHeader title="Infrastructure" sub="Live stats from the running node" />
        {err ? (
          <ErrorNote testid="ops-error" onRetry={load}>
            {err}
          </ErrorNote>
        ) : (
          <div className="sr-only" role="status">
            Loading infrastructure stats…
          </div>
        )}
        <div className="grid gap-4 lg:grid-cols-2">
          <Skeleton className="h-72 w-full" />
          <Skeleton className="h-72 w-full" />
        </div>
      </div>
    );

  const { nodeName } = systemInfoFrom(s);
  const up = uptime(s.uptime_s);
  const sinceRestart = `since restart ${up} ago`;
  const shardData = s.shards.map((x) => ({
    name: `Shard ${x.shard}`,
    clicks: x.clicks,
  }));
  const lookups = (s.cache.hits || 0) + (s.cache.misses || 0);
  const goVersion = String(s.runtime.go_version || "").replace(/^go/i, "");

  // Newest day first; the card grows when expanded instead of clipping rows.
  const parts = [...s.olap.partitions].reverse();
  const shownParts = allParts ? parts : parts.slice(0, PARTITIONS_COLLAPSED);
  const maxPartRows = Math.max(1, ...parts.map((p) => p.rows));

  return (
    <div className="space-y-5" data-testid="ops-page">
      <PageHeader
        title="Infrastructure"
        sub={
          <>
            Live stats from node{" "}
            <span className="font-mono text-kx-ink2">{nodeName || "kortex"}</span>, refreshed every
            3 seconds
          </>
        }
        right={
          <div className="flex flex-wrap gap-2">
            <Chip tone={err ? "negative" : "positive"}>
              <span
                className={`h-1.5 w-1.5 rounded-full ${err ? "bg-kx-negative" : "bg-kx-positive"}`}
              />
              {err ? "Unreachable" : "Healthy"}
            </Chip>
            <Chip>Up {up}</Chip>
            {goVersion && <Chip>Go {goVersion}</Chip>}
            <Chip>{s.runtime.cpus} vCPU</Chip>
            <Chip>
              {s.redirects
                ? `${count(s.redirects, "redirect")} since restart`
                : "No redirects since restart"}
            </Chip>
          </div>
        }
      />
      {err && (
        <ErrorNote testid="ops-error" onRetry={load}>
          {err}. Showing the last good snapshot.
        </ErrorNote>
      )}

      <div className="grid gap-4 lg:grid-cols-2">
        <Panel
          testid="ops-cache"
          title="Redis cache"
          sub={`Read-path cache, ${s.cache.eviction_policy} eviction`}
        >
          <Ring
            value={s.cache.hit_rate}
            empty={lookups === 0}
            label="Cache hit rate"
            sub={
              lookups === 0
                ? `No lookups ${sinceRestart}`
                : `${count(s.cache.hits, "hit")} / ${count(s.cache.misses, "miss", "misses")} ${sinceRestart}`
            }
          />
          <div className="mt-5 grid grid-cols-2 gap-2 sm:grid-cols-4">
            <Figure label="Keys resident" value={fmt.format(s.cache.keys)} />
            <Figure label="Warmed at boot" value={fmt.format(s.cache.warmed)} />
            <Figure label="Evicted" value={fmt.format(s.cache.evicted_keys)} />
            <Figure label="Memory" value={s.cache.used_memory_mb.toFixed(1)} unit="MB" />
          </div>
          <p className="mt-4 text-xs leading-relaxed text-kx-muted">
            Boot warms the hottest codes so the first traffic after a deploy never stampedes
            Postgres. With allkeys-lru the cold tail is evicted first, so the working set stays
            resident under memory pressure.
          </p>
        </Panel>

        <Panel
          testid="ops-bus"
          title="Event log"
          sub={
            <>
              Topic <span className="font-mono">{s.bus.topic}</span>, consumer group{" "}
              <span className="font-mono">clickhouse-sink</span> · counts {sinceRestart}
            </>
          }
        >
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
            <Figure label="Produced" value={compact(s.bus.produced)} />
            <Figure label="Consumed" value={compact(s.bus.consumed)} />
            <Figure
              label="Total lag"
              value={fmt.format(s.bus.total_lag)}
              tone={s.bus.total_lag > 0 ? "text-kx-warning" : "text-kx-ink"}
            />
            <Figure
              label="Dropped"
              value={fmt.format(s.bus.dropped)}
              tone={s.bus.dropped ? "text-kx-negative" : "text-kx-ink"}
            />
          </div>
          {s.bus.produced === 0 && (
            <p className="mt-3 text-xs text-kx-muted">
              No click events {sinceRestart}. Counts start again from zero on every restart.
            </p>
          )}
          <div className="mt-4 overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-kx-line">
                  <th className={`${th} text-left`}>Partition</th>
                  <th className={`${th} text-right`}>High water</th>
                  <th className={`${th} text-right`}>Committed</th>
                  <th className={`${th} text-right`}>Lag</th>
                  <th className={`${th} text-right`}>Retained</th>
                </tr>
              </thead>
              <tbody>
                {s.bus.partitions.map((p) => (
                  <tr key={p.id} className="border-b border-kx-line last:border-b-0">
                    <td className="py-2 font-mono text-xs text-kx-ink2">clicks-{p.id}</td>
                    <td className="kx-num py-2 text-right text-kx-ink">
                      {fmt.format(p.high_watermark)}
                    </td>
                    <td className="kx-num py-2 text-right text-kx-ink">
                      {fmt.format(p.committed)}
                    </td>
                    <td
                      className={`kx-num py-2 text-right ${p.lag > 0 ? "text-kx-warning" : "text-kx-ink"}`}
                    >
                      {fmt.format(p.lag)}
                    </td>
                    <td className="kx-num py-2 text-right text-kx-ink">{fmt.format(p.retained)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Panel>
      </div>

      <div className="grid gap-4 lg:grid-cols-[1.2fr_1fr]">
        <Panel
          testid="ops-olap"
          title="Columnar store"
          sub={`MergeTree · ${count(s.olap.total_rows, "row")} · ${count(s.olap.part_count, "part")} · ${count(s.olap.merges, "merge")}`}
        >
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
            <Figure label="Day partitions" value={fmt.format(s.olap.partitions.length)} />
            <Figure label="View rows" value={compact(s.olap.mv_rows)} />
            <Figure label="Dictionary" value={fmt.format(s.olap.dict_entries)} />
            <Figure label="Memory" value={s.olap.approx_mem_mb.toFixed(1)} unit="MB" />
          </div>
          <table className="mt-4 w-full text-sm">
            <thead>
              <tr className="border-b border-kx-line">
                <th className={`${th} text-left`}>Partition</th>
                <th className={`${th} text-right`}>Parts</th>
                <th className={`${th} text-right`}>Rows</th>
                <th className={`${th} w-[34%] pl-4 text-left`}>Share</th>
              </tr>
            </thead>
            <tbody>
              {shownParts.map((p) => {
                const share = s.olap.total_rows ? (p.rows / s.olap.total_rows) * 100 : 0;
                return (
                  <tr key={p.day} className="border-b border-kx-line last:border-b-0">
                    <td className="kx-num py-1.5 font-mono text-xs text-kx-ink2">{p.day}</td>
                    <td className="kx-num py-1.5 text-right text-kx-ink">{p.parts}</td>
                    <td className="kx-num py-1.5 text-right text-kx-ink">{fmt.format(p.rows)}</td>
                    <td className="py-1.5 pl-4">
                      <div className="flex items-center gap-2">
                        <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-kx-hover">
                          <div
                            className="h-full rounded-full bg-kx-accent"
                            style={{ width: `${(p.rows / maxPartRows) * 100}%` }}
                          />
                        </div>
                        <span className="kx-num w-11 text-right text-xs text-kx-muted">
                          {share.toFixed(1)}%
                        </span>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          {parts.length > PARTITIONS_COLLAPSED && (
            <button
              type="button"
              onClick={() => setAllParts((v) => !v)}
              aria-expanded={allParts}
              data-testid="ops-partitions-toggle"
              className="kx-btn mt-2 rounded-control px-2 py-1 text-kx-accent hover:bg-kx-hover"
            >
              {allParts ? "Show fewer" : `Show all ${parts.length} partitions`}
            </button>
          )}
        </Panel>

        <Panel
          testid="ops-shards"
          title={`Postgres shards · ${s.shard_count ?? s.shards.length}`}
          sub="Consistent hash ring, 160 virtual nodes per shard"
        >
          <div className="h-[190px]">
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={shardData} margin={{ top: 4, left: -12, right: 0, bottom: 0 }}>
                <CartesianGrid stroke={c.grid} vertical={false} />
                <XAxis
                  dataKey="name"
                  tick={{ fill: c.muted, fontSize: 11 }}
                  tickLine={false}
                  axisLine={{ stroke: c.line }}
                />
                <YAxis
                  tick={{ fill: c.muted, fontSize: 11 }}
                  tickLine={false}
                  axisLine={false}
                  tickFormatter={compact}
                />
                <Tooltip content={<ChartTip />} cursor={{ fill: c.hover }} />
                <Bar
                  dataKey="clicks"
                  name="Clicks"
                  fill={c.accent}
                  radius={[3, 3, 0, 0]}
                  maxBarSize={56}
                  isAnimationActive={false}
                />
              </BarChart>
            </ResponsiveContainer>
          </div>
          <div className="mt-4 grid grid-cols-2 gap-2">
            {s.shards.map((x) => (
              <div key={x.shard} className="rounded-control bg-kx-card px-3 py-2">
                <div className="font-mono text-xs text-kx-muted">{x.table}</div>
                <div className="kx-num mt-0.5 text-sm text-kx-ink">
                  {count(x.links, "link")} · {compact(x.clicks)} {plural(x.clicks, "click")}
                </div>
              </div>
            ))}
          </div>
        </Panel>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <Panel testid="ops-idgen" title="ID generator" pad="px-5 py-2">
          <Row k="IDs minted" v={fmt.format(s.id_gen.issued)} />
          <Row k="Node ID" v={s.id_gen.node_id} />
          <Row k="Sequence exhausted" v={fmt.format(s.id_gen.seq_exhausted)} />
          <Row k="Clock rollbacks" v={fmt.format(s.id_gen.clock_backwards)} />
        </Panel>
        <Panel testid="ops-bloom" title="Bloom filter" pad="px-5 py-2">
          <Row k="Bits (m)" v={compact(s.bloom.m)} />
          <Row k="Hashes (k)" v={s.bloom.k} />
          <Row k="Memory" v={`${fmt.format(Math.round(s.bloom.memory_kb))} KB`} />
          <Row k="Est. false positives" v={`${(s.bloom.est_fpr * 100).toFixed(4)}%`} />
          <Row k="DB probes saved" v={fmt.format(s.bloom.db_skipped)} />
        </Panel>
        <Panel testid="ops-pool" title="Postgres pool" pad="px-5 py-2">
          <Row k="Total connections" v={s.pg_pool.total_conns} />
          <Row k="Idle" v={s.pg_pool.idle_conns} />
          <Row k="Acquired" v={s.pg_pool.acquired_conns} />
          <Row k="Max" v={s.pg_pool.max_conns} />
          <Row k="Acquires" v={fmt.format(s.pg_pool.acquire_count)} />
        </Panel>
        <Panel testid="ops-runtime" title="Go runtime" pad="px-5 py-2">
          <Row k="Goroutines" v={fmt.format(s.runtime.goroutines)} />
          <Row k="Heap" v={`${s.runtime.heap_mb.toFixed(1)} MB`} />
          <Row k="GC cycles" v={fmt.format(s.runtime.gc_cycles)} />
          <Row k="vCPU" v={s.runtime.cpus} />
        </Panel>
      </div>
    </div>
  );
}
