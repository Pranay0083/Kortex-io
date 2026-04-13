import { useEffect, useState } from "react";
import { Bar, BarChart, Cell, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { Cpu, Database, HardDrive, Layers, Radio, Server, Sparkles } from "lucide-react";
import { api, CHART_COLORS, compact, fmt } from "@/lib/api";
import { Panel, Stat, Chip, Empty, ChartTip, Bar as MiniBar } from "@/components/kx";

const RACK_IMG = "https://images.pexels.com/photos/17323801/pexels-photo-17323801.jpeg";

function Gauge({ value, label, sub, color = "#00FF66" }) {
  const pct = Math.max(0, Math.min(100, value));
  const r = 52;
  const c = 2 * Math.PI * r;
  return (
    <div className="flex items-center gap-5">
      <svg width="124" height="124" viewBox="0 0 124 124" className="-rotate-90">
        <circle cx="62" cy="62" r={r} fill="none" stroke="#1f1f1f" strokeWidth="9" />
        <circle
          cx="62"
          cy="62"
          r={r}
          fill="none"
          stroke={color}
          strokeWidth="9"
          strokeDasharray={c}
          strokeDashoffset={c - (pct / 100) * c}
          style={{ transition: "stroke-dashoffset 800ms cubic-bezier(0.16,1,0.3,1)" }}
        />
      </svg>
      <div>
        <div className="kx-num text-4xl font-bold" style={{ color }}>
          {pct.toFixed(2)}
          <span className="text-lg text-zinc-600">%</span>
        </div>
        <div className="kx-label mt-1">{label}</div>
        <div className="mt-1 font-mono text-[10px] text-zinc-600">{sub}</div>
      </div>
    </div>
  );
}

export default function Ops() {
  const [s, setS] = useState(null);

  useEffect(() => {
    const load = () => api.get("/system/stats").then(({ data }) => setS(data)).catch(() => {});
    load();
    const t = setInterval(load, 3000);
    return () => clearInterval(t);
  }, []);

  if (!s)
    return (
      <div data-testid="ops-page">
        <Empty>reading /api/system/stats…</Empty>
      </div>
    );

  const shardData = s.shards.map((x) => ({ name: `s${x.shard}`, links: x.links, clicks: x.clicks }));

  return (
    <div className="space-y-4" data-testid="ops-page">
      <div className="relative overflow-hidden border border-kx-line">
        <div
          className="absolute inset-0 bg-cover bg-center opacity-[0.12]"
          style={{ backgroundImage: `url(${RACK_IMG})` }}
        />
        <div className="absolute inset-0 bg-gradient-to-r from-black via-black/80 to-transparent" />
        <div className="relative p-6 lg:p-8">
          <div className="kx-label">Console / Infra</div>
          <h1 className="mt-1.5 font-display text-3xl font-black tracking-tighter lg:text-4xl">
            {s.node}
          </h1>
          <div className="mt-4 flex flex-wrap gap-2">
            <Chip tone="green">
              <span className="h-1.5 w-1.5 animate-kx-pulse bg-kx-green" /> healthy
            </Chip>
            <Chip>uptime {Math.floor(s.uptime_s / 60)}m {s.uptime_s % 60}s</Chip>
            <Chip>{s.runtime.go_version}</Chip>
            <Chip>{s.runtime.cpus} vCPU</Chip>
            <Chip tone="cyan">{fmt.format(s.redirects)} redirects served</Chip>
          </div>
        </div>
      </div>

      <div className="grid gap-3 lg:grid-cols-[1fr_1fr]">
        <Panel testid="ops-cache" title="redis · read path cache" sub={`policy ${s.cache.eviction_policy}`}>
          <Gauge
            value={s.cache.hit_rate}
            label="cache hit rate"
            sub={`${fmt.format(s.cache.hits)} hits / ${fmt.format(s.cache.misses)} misses`}
          />
          <div className="mt-5 grid grid-cols-2 gap-2 font-mono text-[10.5px] sm:grid-cols-4">
            {[
              ["keys resident", fmt.format(s.cache.keys)],
              ["warmed at boot", fmt.format(s.cache.warmed)],
              ["evicted", fmt.format(s.cache.evicted_keys)],
              ["used memory", `${s.cache.used_memory_mb.toFixed(1)} MB`],
            ].map(([k, v]) => (
              <div key={k} className="border border-kx-line px-2.5 py-2">
                <div className="text-zinc-600">{k}</div>
                <div className="mt-0.5 text-white">{v}</div>
              </div>
            ))}
          </div>
          <p className="mt-4 font-mono text-[10px] leading-relaxed text-zinc-600">
            Boot warms the hottest codes so the first traffic after a deploy never stampedes Postgres.
            allkeys-lru means the cold tail is evicted first and the working set stays resident under
            memory pressure.
          </p>
        </Panel>

        <Panel testid="ops-bus" title={`event log · topic ${s.bus.topic}`} sub="consumer group clickhouse-sink">
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
            <Stat label="produced" value={compact(s.bus.produced)} accent="#00E5FF" />
            <Stat label="consumed" value={compact(s.bus.consumed)} accent="#00FF66" />
            <Stat label="total lag" value={fmt.format(s.bus.total_lag)} accent="#FFD60A" />
            <Stat label="dropped" value={fmt.format(s.bus.dropped)} accent={s.bus.dropped ? "#FF3B30" : "#FFFFFF"} />
          </div>
          <table className="mt-5 w-full font-mono text-[10.5px]">
            <thead>
              <tr className="border-b border-kx-line">
                {["partition", "high water", "committed", "lag", "retained"].map((h) => (
                  <th key={h} className="kx-label py-2 text-left">
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {s.bus.partitions.map((p) => (
                <tr key={p.id} className="border-b border-kx-line/50">
                  <td className="py-2 text-kx-cyan">clicks-{p.id}</td>
                  <td className="py-2 text-zinc-400">{fmt.format(p.high_watermark)}</td>
                  <td className="py-2 text-zinc-400">{fmt.format(p.committed)}</td>
                  <td className={`py-2 ${p.lag > 0 ? "text-kx-yellow" : "text-kx-green"}`}>{p.lag}</td>
                  <td className="py-2 text-zinc-500">{fmt.format(p.retained)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </Panel>
      </div>

      <div className="grid gap-3 lg:grid-cols-[1.2fr_1fr]">
        <Panel
          testid="ops-olap"
          title="columnar store · MergeTree"
          sub={`${fmt.format(s.olap.total_rows)} rows · ${s.olap.part_count} parts · ${s.olap.merges} merges`}
        >
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
            <Stat label="day partitions" value={s.olap.partitions.length} accent="#8A2BE2" />
            <Stat label="mv rows" value={compact(s.olap.mv_rows)} accent="#00FF66" />
            <Stat label="dictionary" value={fmt.format(s.olap.dict_entries)} />
            <Stat label="approx memory" value={s.olap.approx_mem_mb.toFixed(1)} unit="MB" />
          </div>
          <div className="mt-5 max-h-[220px] overflow-y-auto">
            <table className="w-full font-mono text-[10.5px]">
              <thead className="sticky top-0 bg-kx-surface">
                <tr className="border-b border-kx-line">
                  {["partition", "parts", "rows", "share"].map((h) => (
                    <th key={h} className="kx-label py-2 text-left">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {s.olap.partitions.map((p) => (
                  <tr key={p.day} className="border-b border-kx-line/40">
                    <td className="py-1.5 text-zinc-300">{p.day}</td>
                    <td className="py-1.5 text-zinc-500">{p.parts}</td>
                    <td className="py-1.5 text-zinc-400">{fmt.format(p.rows)}</td>
                    <td className="w-1/3 py-1.5">
                      <MiniBar pct={(p.rows / s.olap.total_rows) * 700} color="#8A2BE2" />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Panel>

        <Panel testid="ops-shards" title="postgres shards" sub="consistent hash ring, 160 vnodes each">
          <div className="h-[190px]">
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={shardData} margin={{ left: -20, right: 8 }}>
                <XAxis
                  dataKey="name"
                  tick={{ fill: "#a1a1aa", fontSize: 10, fontFamily: "JetBrains Mono" }}
                  tickLine={false}
                  axisLine={{ stroke: "#222" }}
                />
                <YAxis
                  tick={{ fill: "#52525b", fontSize: 10, fontFamily: "JetBrains Mono" }}
                  tickLine={false}
                  axisLine={false}
                  tickFormatter={compact}
                />
                <Tooltip content={<ChartTip />} cursor={{ fill: "#141414" }} />
                <Bar dataKey="clicks" name="clicks" isAnimationActive={false}>
                  {shardData.map((_, i) => (
                    <Cell key={i} fill={CHART_COLORS[i % CHART_COLORS.length]} />
                  ))}
                </Bar>
              </BarChart>
            </ResponsiveContainer>
          </div>
          <div className="mt-3 grid grid-cols-2 gap-2 font-mono text-[10.5px]">
            {s.shards.map((x) => (
              <div key={x.shard} className="border border-kx-line px-2.5 py-2">
                <div className="text-zinc-600">{x.table}</div>
                <div className="mt-0.5 text-white">
                  {x.links} links · {compact(x.clicks)} clicks
                </div>
              </div>
            ))}
          </div>
        </Panel>
      </div>

      <div className="grid gap-3 lg:grid-cols-4">
        <Panel testid="ops-idgen" title="id generator">
          <div className="space-y-2 font-mono text-[11px]">
            <Row icon={Sparkles} k="ids minted" v={fmt.format(s.id_gen.issued)} />
            <Row icon={Server} k="node id" v={s.id_gen.node_id} />
            <Row icon={Layers} k="seq exhausted" v={s.id_gen.seq_exhausted} />
            <Row icon={Radio} k="clock rollback" v={s.id_gen.clock_backwards} />
          </div>
        </Panel>
        <Panel testid="ops-bloom" title="bloom filter">
          <div className="space-y-2 font-mono text-[11px]">
            <Row icon={Database} k="bits (m)" v={compact(s.bloom.m)} />
            <Row icon={Layers} k="hashes (k)" v={s.bloom.k} />
            <Row icon={HardDrive} k="memory" v={`${s.bloom.memory_kb.toFixed(0)} KB`} />
            <Row icon={Sparkles} k="est. fpr" v={`${(s.bloom.est_fpr * 100).toFixed(4)}%`} />
            <Row icon={Cpu} k="db probes saved" v={fmt.format(s.bloom.db_skipped)} />
          </div>
        </Panel>
        <Panel testid="ops-pool" title="pg pool">
          <div className="space-y-2 font-mono text-[11px]">
            <Row icon={Database} k="total conns" v={s.pg_pool.total_conns} />
            <Row icon={Database} k="idle" v={s.pg_pool.idle_conns} />
            <Row icon={Database} k="acquired" v={s.pg_pool.acquired_conns} />
            <Row icon={Database} k="max" v={s.pg_pool.max_conns} />
            <Row icon={Database} k="acquires" v={fmt.format(s.pg_pool.acquire_count)} />
          </div>
        </Panel>
        <Panel testid="ops-runtime" title="go runtime">
          <div className="space-y-2 font-mono text-[11px]">
            <Row icon={Cpu} k="goroutines" v={s.runtime.goroutines} />
            <Row icon={HardDrive} k="heap" v={`${s.runtime.heap_mb.toFixed(1)} MB`} />
            <Row icon={Layers} k="gc cycles" v={s.runtime.gc_cycles} />
            <Row icon={Server} k="vcpu" v={s.runtime.cpus} />
          </div>
        </Panel>
      </div>
    </div>
  );
}

const Row = ({ icon: Icon, k, v }) => (
  <div className="flex items-center justify-between border-b border-kx-line/50 pb-1.5">
    <span className="flex items-center gap-2 text-zinc-500">
      <Icon size={11} strokeWidth={1.5} className="text-zinc-700" />
      {k}
    </span>
    <span className="text-white">{v}</span>
  </div>
);
