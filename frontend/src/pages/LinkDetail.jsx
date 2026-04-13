import { useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { toast } from "sonner";
import { ArrowLeft, Copy, ExternalLink, Zap } from "lucide-react";
import { api, apiError, API, CHART_COLORS, compact, fmt, shortUrl, prettyUrl, ago } from "@/lib/api";
import { Panel, Stat, Chip, Button, Empty, ChartTip, Bar as MiniBar } from "@/components/kx";

const RANGES = ["1h", "6h", "24h", "7d", "30d"];
const DIMS = ["country", "city", "referrer", "device", "browser", "os"];

function Race({ code }) {
  const [res, setRes] = useState(null);
  const [busy, setBusy] = useState(false);

  const run = async () => {
    setBusy(true);
    try {
      const { data } = await api.get(`/analytics/${code}/race?range=7d`);
      setRes(data);
    } catch (e) {
      toast.error(apiError(e));
    }
    setBusy(false);
  };

  const max = res ? Math.max(res.raw.us, res.mv.us) : 1;

  return (
    <Panel
      testid="race-panel"
      title="materialized view vs on-read aggregation"
      sub="identical question, two execution plans, over the rows in the store right now"
      right={
        <Button variant="solid" onClick={run} disabled={busy} data-testid="run-race-btn">
          <Zap size={12} strokeWidth={2} /> {busy ? "racing…" : "Race"}
        </Button>
      }
    >
      {!res ? (
        <Empty>press race to execute both plans</Empty>
      ) : (
        <div className="space-y-4" data-testid="race-result">
          {[
            { k: "raw", label: "on-read full scan", color: "#FF3B30", d: res.raw },
            { k: "mv", label: "pre-aggregated rollup", color: "#00FF66", d: res.mv },
          ].map((r) => (
            <div key={r.k}>
              <div className="flex items-baseline justify-between font-mono text-[11px]">
                <span className="text-zinc-300">{r.label}</span>
                <span className="font-bold" style={{ color: r.color }} data-testid={`race-${r.k}-ms`}>
                  {(r.d.us / 1000).toFixed(3)} ms
                </span>
              </div>
              <div className="mt-1.5 h-2 w-full bg-kx-line">
                <div
                  className="h-full transition-[width] duration-700"
                  style={{ width: `${(r.d.us / max) * 100}%`, backgroundColor: r.color }}
                />
              </div>
              <div className="mt-1 font-mono text-[10px] text-zinc-600">
                scanned {fmt.format(r.d.rows_scanned)} values · {fmt.format(r.d.total)} clicks ·{" "}
                {r.d.buckets} buckets
              </div>
            </div>
          ))}
          <div className="grid grid-cols-3 gap-2 border-t border-kx-line pt-4">
            <div>
              <div className="kx-label">speedup</div>
              <div className="kx-num mt-1 text-2xl font-bold text-kx-cyan" data-testid="race-speedup">
                {res.speedup.toFixed(1)}×
              </div>
            </div>
            <div>
              <div className="kx-label">table rows</div>
              <div className="kx-num mt-1 text-2xl font-bold">{compact(res.table_rows)}</div>
            </div>
            <div>
              <div className="kx-label">results match</div>
              <div
                className="kx-num mt-1 text-2xl font-bold"
                style={{ color: res.identical ? "#00FF66" : "#FFD60A" }}
              >
                {res.identical ? "yes" : "±drift"}
              </div>
            </div>
          </div>
        </div>
      )}
    </Panel>
  );
}

export default function LinkDetail() {
  const { code } = useParams();
  const [link, setLink] = useState(null);
  const [sf, setSf] = useState(null);
  const [range, setRange] = useState("7d");
  const [gran, setGran] = useState("");
  const [series, setSeries] = useState([]);
  const [queryUs, setQueryUs] = useState(0);
  const [summary, setSummary] = useState(null);
  const [dim, setDim] = useState("country");
  const [rows, setRows] = useState([]);

  useEffect(() => {
    api
      .get(`/links/${code}`)
      .then(({ data }) => {
        setLink(data.link);
        setSf(data.snowflake);
      })
      .catch((e) => toast.error(apiError(e)));
  }, [code]);

  const loadSeries = useCallback(async () => {
    try {
      const g = gran ? `&gran=${gran}` : "";
      const [ts, sm] = await Promise.all([
        api.get(`/analytics/${code}/timeseries?range=${range}${g}`),
        api.get(`/analytics/${code}/summary`),
      ]);
      setSeries(ts.data.points || []);
      setQueryUs(ts.data.query_us);
      setSummary(sm.data);
    } catch {
      /* ignore */
    }
  }, [code, range, gran]);

  useEffect(() => {
    loadSeries();
    const t = setInterval(loadSeries, 8000);
    return () => clearInterval(t);
  }, [loadSeries]);

  useEffect(() => {
    api
      .get(`/analytics/${code}/breakdown?dim=${dim}&limit=12`)
      .then(({ data }) => setRows(data.rows || []))
      .catch(() => setRows([]));
  }, [code, dim, range]);

  return (
    <div className="space-y-4" data-testid="link-detail-page">
      <Link
        to="/app/links"
        className="inline-flex items-center gap-2 font-mono text-[11px] uppercase tracking-widest text-zinc-500 hover:text-kx-cyan"
        data-testid="back-to-links"
      >
        <ArrowLeft size={12} /> links
      </Link>

      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="min-w-0">
          <div className="kx-label">
            {link ? `links_s${link.shard} · ${link.redirect_type} redirect` : "loading"}
          </div>
          <h1 className="mt-1.5 font-display text-3xl font-black tracking-tighter lg:text-4xl">
            /{code}
          </h1>
          {link && (
            <a
              href={link.long_url}
              target="_blank"
              rel="noreferrer"
              className="mt-1.5 flex items-center gap-1.5 truncate font-mono text-[11px] text-zinc-500 hover:text-kx-cyan"
            >
              {link.long_url} <ExternalLink size={10} />
            </a>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button
            variant="ghost"
            data-testid="detail-copy"
            onClick={() => {
              navigator.clipboard.writeText(shortUrl(code));
              toast.success("Short URL copied");
            }}
          >
            <Copy size={12} /> copy
          </Button>
          <a href={shortUrl(code)} target="_blank" rel="noreferrer" data-testid="detail-open">
            <Button variant="primary">
              <ExternalLink size={12} /> visit
            </Button>
          </a>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
        <Stat
          testid="detail-total"
          label="total clicks"
          value={summary ? compact(summary.total) : "—"}
          accent="#00E5FF"
        />
        <Stat
          testid="detail-24h"
          label="last 24h"
          value={summary ? compact(summary.last_24h) : "—"}
          accent="#00FF66"
        />
        <Stat testid="detail-1h" label="last hour" value={summary ? compact(summary.last_1h) : "—"} />
        <Stat
          testid="detail-bots"
          label="bots"
          value={summary ? compact(summary.bots) : "—"}
          accent="#FFD60A"
        />
        <Stat testid="detail-last" label="last event" value={summary ? ago(summary.last_seen) : "—"} />
      </div>

      <Panel
        testid="detail-timeseries"
        title="click volume"
        sub={`rollup answered in ${(queryUs / 1000).toFixed(3)} ms`}
        right={
          <div className="flex flex-wrap gap-2">
            <div className="flex border border-kx-line">
              {RANGES.map((r) => (
                <button
                  key={r}
                  data-testid={`detail-range-${r}`}
                  onClick={() => setRange(r)}
                  className={`kx-btn px-2.5 py-1.5 ${
                    range === r ? "bg-kx-cyan text-black" : "text-zinc-500 hover:text-white"
                  }`}
                >
                  {r}
                </button>
              ))}
            </div>
            <div className="flex border border-kx-line">
              {["", "minute", "hour", "day"].map((g) => (
                <button
                  key={g || "auto"}
                  data-testid={`detail-gran-${g || "auto"}`}
                  onClick={() => setGran(g)}
                  className={`kx-btn px-2.5 py-1.5 ${
                    gran === g ? "bg-white text-black" : "text-zinc-500 hover:text-white"
                  }`}
                >
                  {g || "auto"}
                </button>
              ))}
            </div>
          </div>
        }
      >
        <div className="h-[260px]">
          {series.length === 0 ? (
            <Empty>no clicks in this window</Empty>
          ) : (
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={series} margin={{ top: 6, right: 4, left: -22, bottom: 0 }}>
                <defs>
                  <linearGradient id="gDetail" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="#00FF66" stopOpacity={0.4} />
                    <stop offset="100%" stopColor="#00FF66" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid stroke="#1b1b1b" vertical={false} />
                <XAxis
                  dataKey="label"
                  tick={{ fill: "#52525b", fontSize: 10, fontFamily: "JetBrains Mono" }}
                  tickLine={false}
                  axisLine={{ stroke: "#222" }}
                  minTickGap={30}
                />
                <YAxis
                  tick={{ fill: "#52525b", fontSize: 10, fontFamily: "JetBrains Mono" }}
                  tickLine={false}
                  axisLine={false}
                  tickFormatter={compact}
                />
                <Tooltip content={<ChartTip />} cursor={{ stroke: "#00FF66", strokeWidth: 1 }} />
                <Area
                  type="linear"
                  dataKey="count"
                  name="clicks"
                  stroke="#00FF66"
                  strokeWidth={1.6}
                  fill="url(#gDetail)"
                  dot={false}
                  isAnimationActive={false}
                />
              </AreaChart>
            </ResponsiveContainer>
          )}
        </div>
      </Panel>

      <div className="grid gap-3 lg:grid-cols-2">
        <Panel
          testid="detail-breakdown"
          title="dimension breakdown"
          sub={`mv_clicks_by_${dim}`}
          right={
            <div className="flex flex-wrap gap-1">
              {DIMS.map((d) => (
                <button
                  key={d}
                  data-testid={`dim-${d}`}
                  onClick={() => setDim(d)}
                  className={`kx-btn border px-2 py-1 ${
                    dim === d ? "border-kx-cyan text-kx-cyan" : "border-kx-line text-zinc-500"
                  }`}
                >
                  {d}
                </button>
              ))}
            </div>
          }
        >
          <div className="h-[280px]">
            {rows.length === 0 ? (
              <Empty>no data</Empty>
            ) : (
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={rows} layout="vertical" margin={{ left: 46, right: 12 }}>
                  <XAxis type="number" hide />
                  <YAxis
                    type="category"
                    dataKey="key"
                    width={120}
                    tick={{ fill: "#a1a1aa", fontSize: 10, fontFamily: "JetBrains Mono" }}
                    tickLine={false}
                    axisLine={false}
                  />
                  <Tooltip content={<ChartTip />} cursor={{ fill: "#141414" }} />
                  <Bar dataKey="count" name="clicks" isAnimationActive={false}>
                    {rows.map((_, i) => (
                      <Cell key={i} fill={CHART_COLORS[i % CHART_COLORS.length]} />
                    ))}
                  </Bar>
                </BarChart>
              </ResponsiveContainer>
            )}
          </div>
        </Panel>

        <div className="space-y-3">
          <Race code={code} />
          {sf && (
            <Panel testid="detail-snowflake" title="id anatomy" sub="decoded client-side from the code itself">
              <div className="flex overflow-hidden font-mono text-[9px]">
                <span className="bg-kx-cyan/20 px-1 py-1.5 text-kx-cyan">{sf.time_bits}</span>
                <span className="bg-kx-green/20 px-1 py-1.5 text-kx-green">{sf.node_bits}</span>
                <span className="bg-kx-yellow/20 px-1 py-1.5 text-kx-yellow">{sf.seq_bits}</span>
              </div>
              <div className="mt-3 grid grid-cols-2 gap-2 font-mono text-[10.5px] sm:grid-cols-4">
                {[
                  ["id", sf.id],
                  ["node", sf.node],
                  ["seq", sf.sequence],
                  ["minted", new Date(sf.timestamp_ms).toISOString().slice(0, 19).replace("T", " ")],
                ].map(([k, v]) => (
                  <div key={k} className="border border-kx-line px-2.5 py-2">
                    <div className="text-zinc-600">{k}</div>
                    <div className="mt-0.5 truncate text-white">{v}</div>
                  </div>
                ))}
              </div>
            </Panel>
          )}
        </div>
      </div>

      <div className="grid gap-3 lg:grid-cols-[1fr_240px]">
        <Panel testid="detail-toplist" title={`top ${dim}`} pad="p-0">
          <div className="divide-y divide-kx-line/60">
            {rows.slice(0, 10).map((r, i) => (
              <div key={r.key} className="px-5 py-2.5">
                <div className="flex items-baseline justify-between font-mono text-[11px]">
                  <span className="text-zinc-300">{r.key}</span>
                  <span className="text-zinc-500">
                    {fmt.format(r.count)} · {r.pct.toFixed(1)}%
                  </span>
                </div>
                <div className="mt-1.5">
                  <MiniBar pct={r.pct * 2} color={CHART_COLORS[i % CHART_COLORS.length]} />
                </div>
              </div>
            ))}
          </div>
        </Panel>
        <Panel testid="detail-qr" title="qr">
          <img
            alt="QR"
            className="w-full border border-kx-line bg-white p-2"
            src={`${API}/qr/${code}?url=${encodeURIComponent(prettyUrl(code))}`}
          />
          <div className="mt-3 flex flex-wrap gap-1.5">
            <Chip tone="cyan">{link?.custom ? "custom alias" : "generated"}</Chip>
            <Chip>{link?.active ? "active" : "disabled"}</Chip>
          </div>
        </Panel>
      </div>
    </div>
  );
}
