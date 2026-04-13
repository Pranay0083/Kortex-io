import { useCallback, useEffect, useRef, useState } from "react";
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { Activity, Globe, MousePointerClick, Radio, Timer, Users } from "lucide-react";
import { api, CHART_COLORS, compact, fmt, ago } from "@/lib/api";
import { Panel, Stat, Chip, Empty, ChartTip, Bar as MiniBar } from "@/components/kx";

const RANGES = ["1h", "6h", "24h", "7d", "30d"];

function ClickStream() {
  const [events, setEvents] = useState([]);
  const [bus, setBus] = useState(null);
  const seen = useRef(new Set());

  useEffect(() => {
    let alive = true;
    const tick = async () => {
      try {
        const { data } = await api.get("/stream/recent?limit=24");
        if (!alive) return;
        setEvents(data.events || []);
        setBus(data.bus);
      } catch {
        /* ignore */
      }
    };
    tick();
    const t = setInterval(tick, 2000);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, []);

  return (
    <Panel
      testid="click-stream-panel"
      title="live click stream"
      sub={bus ? `topic ${bus.topic} · ${bus.partitions.length} partitions · lag ${bus.total_lag}` : "connecting…"}
      right={
        <Chip tone={events.length ? "green" : "zinc"}>
          <span className={`h-1.5 w-1.5 ${events.length ? "animate-kx-pulse bg-kx-green" : "bg-zinc-600"}`} />
          {events.length ? "streaming" : "idle"}
        </Chip>
      }
      pad="p-0"
      className="kx-scanlines overflow-hidden"
    >
      <div className="h-[300px] overflow-y-auto">
        {events.length === 0 ? (
          <div className="p-5">
            <Empty>no events yet — hit a short link or run the benchmark</Empty>
          </div>
        ) : (
          <table className="w-full font-mono text-[11px]">
            <tbody>
              {events.map((e, i) => {
                const k = `${e.ts}-${e.code}-${i}`;
                const isNew = !seen.current.has(k);
                seen.current.add(k);
                return (
                  <tr
                    key={k}
                    className={`border-b border-kx-line/50 hover:bg-kx-hover ${isNew ? "kx-row-new" : ""}`}
                  >
                    <td className="px-4 py-1.5 text-zinc-600">
                      {new Date(e.ts).toISOString().slice(11, 23)}
                    </td>
                    <td className="px-2 py-1.5 text-kx-cyan">/{e.code}</td>
                    <td className="px-2 py-1.5 text-zinc-400">{e.country}</td>
                    <td className="hidden px-2 py-1.5 text-zinc-500 sm:table-cell">{e.city}</td>
                    <td className="hidden px-2 py-1.5 text-zinc-500 md:table-cell">{e.referrer}</td>
                    <td className="hidden px-2 py-1.5 text-zinc-500 lg:table-cell">{e.device}</td>
                    <td className="px-2 py-1.5 text-right">
                      <span className={e.cache_hit ? "text-kx-green" : "text-kx-yellow"}>
                        {e.cache_hit ? "HIT" : "MISS"}
                      </span>
                    </td>
                    <td className="px-4 py-1.5 text-right text-zinc-600">{e.status}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>
    </Panel>
  );
}

export default function Dashboard() {
  const [range, setRange] = useState("24h");
  const [ov, setOv] = useState(null);
  const [series, setSeries] = useState([]);
  const [queryUs, setQueryUs] = useState(0);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    try {
      const [o, ts] = await Promise.all([
        api.get("/analytics/overview"),
        api.get(`/analytics/all/timeseries?range=${range}`),
      ]);
      setOv(o.data);
      setSeries(ts.data.points || []);
      setQueryUs(ts.data.query_us);
    } catch {
      /* ignore */
    }
    setLoading(false);
  }, [range]);

  useEffect(() => {
    load();
    const t = setInterval(load, 8000);
    return () => clearInterval(t);
  }, [load]);

  const s = ov?.summary;

  return (
    <div className="space-y-4" data-testid="dashboard-page">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <div className="kx-label">Console / Overview</div>
          <h1 className="mt-1.5 font-display text-3xl font-black tracking-tighter lg:text-4xl">
            Traffic across {ov?.links ?? "—"} links
          </h1>
        </div>
        <div className="flex items-center gap-2">
          <div className="flex border border-kx-line">
            {RANGES.map((r) => (
              <button
                key={r}
                data-testid={`range-${r}`}
                onClick={() => setRange(r)}
                className={`kx-btn px-3 py-2 ${
                  range === r ? "bg-kx-cyan text-black" : "text-zinc-500 hover:text-white"
                }`}
              >
                {r}
              </button>
            ))}
          </div>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
        <Stat
          testid="stat-total-clicks"
          label="total clicks"
          value={s ? compact(s.total) : "—"}
          sub={s ? `${fmt.format(s.total)} rows` : ""}
          accent="#00E5FF"
        />
        <Stat
          testid="stat-clicks-24h"
          label="last 24h"
          value={s ? compact(s.last_24h) : "—"}
          accent="#00FF66"
        />
        <Stat testid="stat-clicks-1h" label="last hour" value={s ? compact(s.last_1h) : "—"} />
        <Stat
          testid="stat-bots"
          label="bot traffic"
          value={s && s.total ? `${((s.bots / s.total) * 100).toFixed(1)}%` : "—"}
          sub={s ? `${fmt.format(s.bots)} events` : ""}
          accent="#FFD60A"
        />
        <Stat
          testid="stat-query-time"
          label="rollup query"
          value={queryUs ? (queryUs / 1000).toFixed(2) : "—"}
          unit="ms"
          sub="materialized view"
          accent="#8A2BE2"
        />
      </div>

      <Panel
        testid="timeseries-panel"
        title={`clicks · ${range}`}
        sub={`served from pre-aggregated rollup in ${(queryUs / 1000).toFixed(2)} ms`}
        right={<Chip tone="cyan">materialized view</Chip>}
      >
        <div className="h-[280px]">
          {loading ? (
            <Empty>loading rollup…</Empty>
          ) : (
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={series} margin={{ top: 6, right: 4, left: -22, bottom: 0 }}>
                <defs>
                  <linearGradient id="gClicks" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="#00E5FF" stopOpacity={0.45} />
                    <stop offset="100%" stopColor="#00E5FF" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid stroke="#1b1b1b" vertical={false} />
                <XAxis
                  dataKey="label"
                  tick={{ fill: "#52525b", fontSize: 10, fontFamily: "JetBrains Mono" }}
                  tickLine={false}
                  axisLine={{ stroke: "#222" }}
                  minTickGap={28}
                />
                <YAxis
                  tick={{ fill: "#52525b", fontSize: 10, fontFamily: "JetBrains Mono" }}
                  tickLine={false}
                  axisLine={false}
                  tickFormatter={compact}
                />
                <Tooltip content={<ChartTip />} cursor={{ stroke: "#00E5FF", strokeWidth: 1 }} />
                <Area
                  type="linear"
                  dataKey="count"
                  name="clicks"
                  stroke="#00E5FF"
                  strokeWidth={1.6}
                  fill="url(#gClicks)"
                  dot={false}
                  isAnimationActive={false}
                />
              </AreaChart>
            </ResponsiveContainer>
          )}
        </div>
      </Panel>

      <div className="grid gap-3 lg:grid-cols-3">
        <Panel testid="geo-panel" title="geography" sub="mv_clicks_by_country">
          {!ov ? (
            <Empty>—</Empty>
          ) : (
            <div className="space-y-3">
              {ov.geo.slice(0, 7).map((r, i) => (
                <div key={r.key}>
                  <div className="flex items-baseline justify-between font-mono text-[11px]">
                    <span className="flex items-center gap-2 text-zinc-300">
                      <Globe size={11} strokeWidth={1.5} className="text-zinc-600" />
                      {r.key}
                    </span>
                    <span className="text-zinc-500">
                      {compact(r.count)} · {r.pct.toFixed(1)}%
                    </span>
                  </div>
                  <div className="mt-1.5">
                    <MiniBar pct={r.pct * 2.6} color={CHART_COLORS[i % CHART_COLORS.length]} />
                  </div>
                </div>
              ))}
            </div>
          )}
        </Panel>

        <Panel testid="referrer-panel" title="referrers" sub="mv_clicks_by_referrer">
          <div className="h-[230px]">
            {!ov ? (
              <Empty>—</Empty>
            ) : (
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={ov.referrer} layout="vertical" margin={{ left: 42, right: 8 }}>
                  <XAxis type="number" hide />
                  <YAxis
                    type="category"
                    dataKey="key"
                    width={110}
                    tick={{ fill: "#a1a1aa", fontSize: 10, fontFamily: "JetBrains Mono" }}
                    tickLine={false}
                    axisLine={false}
                  />
                  <Tooltip content={<ChartTip />} cursor={{ fill: "#141414" }} />
                  <Bar dataKey="count" name="clicks" isAnimationActive={false}>
                    {ov.referrer.map((_, i) => (
                      <Cell key={i} fill={CHART_COLORS[i % CHART_COLORS.length]} />
                    ))}
                  </Bar>
                </BarChart>
              </ResponsiveContainer>
            )}
          </div>
        </Panel>

        <Panel testid="device-panel" title="device mix" sub="mv_clicks_by_device">
          <div className="flex h-[230px] items-center">
            {!ov ? (
              <Empty>—</Empty>
            ) : (
              <>
                <ResponsiveContainer width="55%" height="100%">
                  <PieChart>
                    <Pie
                      data={ov.device}
                      dataKey="count"
                      nameKey="key"
                      innerRadius={42}
                      outerRadius={70}
                      paddingAngle={2}
                      isAnimationActive={false}
                      stroke="#050505"
                    >
                      {ov.device.map((_, i) => (
                        <Cell key={i} fill={CHART_COLORS[i % CHART_COLORS.length]} />
                      ))}
                    </Pie>
                    <Tooltip content={<ChartTip />} />
                  </PieChart>
                </ResponsiveContainer>
                <div className="flex-1 space-y-2">
                  {ov.device.map((d, i) => (
                    <div key={d.key} className="flex items-center gap-2 font-mono text-[11px]">
                      <span
                        className="h-2.5 w-2.5"
                        style={{ background: CHART_COLORS[i % CHART_COLORS.length] }}
                      />
                      <span className="text-zinc-300">{d.key}</span>
                      <span className="ml-auto text-zinc-500">{d.pct.toFixed(1)}%</span>
                    </div>
                  ))}
                  <div className="mt-3 border-t border-kx-line pt-2">
                    {ov.browser.slice(0, 4).map((b) => (
                      <div key={b.key} className="flex justify-between font-mono text-[10px] text-zinc-600">
                        <span>{b.key}</span>
                        <span>{b.pct.toFixed(1)}%</span>
                      </div>
                    ))}
                  </div>
                </div>
              </>
            )}
          </div>
        </Panel>
      </div>

      <ClickStream />

      {s && (
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
          <Stat
            testid="stat-first-seen"
            label="first event"
            value={s.first_seen ? new Date(s.first_seen).toISOString().slice(0, 10) : "—"}
          />
          <Stat testid="stat-last-seen" label="last event" value={ago(s.last_seen)} />
          <Stat testid="stat-links" label="links owned" value={ov.links} />
          <Stat
            testid="stat-overview-query"
            label="overview query"
            value={(ov.query_us / 1000).toFixed(2)}
            unit="ms"
            accent="#00FF66"
          />
        </div>
      )}
    </div>
  );
}
