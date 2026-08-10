import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { toast } from "sonner";
import { ArrowLeft, Copy, Download, ExternalLink, Zap } from "lucide-react";
import { api, apiError, compact, fmt, shortUrl, prettyUrl, qrUrl, ago } from "@/lib/api";
import { copyText } from "@/lib/clipboard";
import { localDateTime, seriesFormatter } from "@/lib/time";
import { usePolling } from "@/hooks/usePolling";
import { useChartColors } from "@/context/ThemeContext";
import {
  Panel,
  Chip,
  Button,
  Empty,
  ChartTip,
  ErrorNote,
  PageHeader,
  Segmented,
  Skeleton,
  Bar as MiniBar,
} from "@/components/kx";

const RANGES = ["1h", "6h", "24h", "7d", "30d"];
const RANGE_LABEL = {
  "1h": "Last hour",
  "6h": "Last 6 hours",
  "24h": "Last 24 hours",
  "7d": "Last 7 days",
  "30d": "Last 30 days",
};
const DIMS = [
  { v: "referrer", l: "Referrer", t: "Top referrers" },
  { v: "device", l: "Device", t: "Devices" },
  { v: "browser", l: "Browser", t: "Browsers" },
  { v: "os", l: "OS", t: "Operating systems" },
];
const GRANS = [
  { v: "", l: "Auto", id: "auto" },
  { v: "minute", l: "Minute" },
  { v: "hour", l: "Hour" },
  { v: "day", l: "Day" },
];

const TICK = { fontSize: 11, fontFamily: '"IBM Plex Sans", sans-serif' };

const longDate = (v) =>
  new Date(v).toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" });
const isPast = (v) => Boolean(v) && new Date(v).getTime() < Date.now();
const share = (part, total) => (total ? `${((part / total) * 100).toFixed(1)}%` : "0%");
const capitalize = (s) => (s ? s.charAt(0).toUpperCase() + s.slice(1) : "Unknown");
const referrerName = (k) => (!k || k === "direct" ? "Direct" : k);

const DIM_LABEL = { referrer: referrerName, device: capitalize };

/** Headline figures as one divided strip; every figure is ink. */
function StatStrip({ items }) {
  return (
    <div className="grid grid-cols-2 gap-px overflow-hidden rounded-card border border-kx-line bg-kx-line lg:grid-cols-4">
      {items.map((it) => (
        <div key={it.testid} data-testid={it.testid} className="min-w-0 bg-kx-surface px-4 py-4">
          <div className="kx-label">{it.label}</div>
          <div
            className={`kx-num mt-1.5 text-2xl font-semibold ${it.value === "—" ? "text-kx-muted" : "text-kx-ink"}`}
          >
            {it.value}
          </div>
          <div data-testid={it.subTestid} className="kx-num mt-1 truncate text-xs text-kx-muted">
            {it.sub || " "}
          </div>
        </div>
      ))}
    </div>
  );
}

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

  return (
    <Panel
      testid="race-panel"
      title="Pre-aggregated vs full scan"
      sub="The same 7-day click count, answered two ways"
      right={
        <Button
          variant="secondary"
          size="sm"
          onClick={run}
          disabled={busy}
          data-testid="run-race-btn"
        >
          <Zap size={14} aria-hidden="true" />
          {busy ? "Running…" : res ? "Run again" : "Run comparison"}
        </Button>
      }
    >
      {!res ? (
        <Empty>Run the comparison to time both ways of answering the same question.</Empty>
      ) : (
        <div data-testid="race-result">
          <div className="grid grid-cols-2 gap-px overflow-hidden rounded-control border border-kx-line bg-kx-line">
            {[
              { k: "raw", label: "Full scan", d: res.raw },
              { k: "mv", label: "Pre-aggregated rollup", d: res.mv },
            ].map((r) => (
              <div key={r.k} className="min-w-0 bg-kx-surface p-4">
                <div className="kx-label">{r.label}</div>
                <div
                  className="kx-num mt-1.5 text-2xl font-semibold text-kx-ink"
                  data-testid={`race-${r.k}-ms`}
                >
                  {(r.d.us / 1000).toFixed(3)}
                  <span className="ml-1 text-sm font-normal text-kx-muted">ms</span>
                </div>
                <div className="kx-num mt-1 text-xs text-kx-muted">
                  Read {fmt.format(r.d.rows_scanned)} values
                </div>
              </div>
            ))}
          </div>
          <div className="mt-4 flex flex-wrap items-center gap-x-3 gap-y-2 text-sm text-kx-ink2">
            <span>
              <span data-testid="race-speedup" className="kx-num font-semibold text-kx-ink">
                {res.speedup.toFixed(1)}×
              </span>{" "}
              faster with the rollup
            </span>
            <span className="kx-num text-kx-muted">
              {fmt.format(res.mv.total)} clicks counted, {compact(res.table_rows)} stored
            </span>
            <Chip tone={res.identical ? "positive" : "warning"}>
              {res.identical ? "Results match" : "Totals differ"}
            </Chip>
          </div>
        </div>
      )}
    </Panel>
  );
}

function IdAnatomy({ sf }) {
  const parts = [
    {
      k: "time",
      label: "Time",
      bits: sf.time_bits,
      cls: "bg-kx-accent/10 text-kx-accent",
      dot: "bg-kx-accent",
    },
    {
      k: "node",
      label: "Node",
      bits: sf.node_bits,
      cls: "bg-kx-hover text-kx-ink",
      dot: "bg-kx-ink",
    },
    { k: "seq", label: "Sequence", bits: sf.seq_bits, cls: "text-kx-muted", dot: "bg-kx-muted" },
  ];
  return (
    <Panel
      testid="detail-snowflake"
      title="ID anatomy"
      sub="The short code decodes to a 64-bit ID made of a timestamp, a node and a sequence number"
    >
      <div className="break-all font-mono text-xs leading-6">
        {parts.map((p) => (
          <span key={p.k} className={`rounded-sm px-0.5 ${p.cls}`}>
            {p.bits}
          </span>
        ))}
      </div>
      <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-kx-muted">
        {parts.map((p) => (
          <span key={p.k} className="inline-flex items-center gap-1.5">
            <span aria-hidden="true" className={`h-2 w-2 rounded-sm ${p.dot}`} />
            {p.label} · {String(p.bits ?? "").length} bits
          </span>
        ))}
      </div>
      <dl className="mt-4 grid grid-cols-2 gap-px overflow-hidden rounded-control border border-kx-line bg-kx-line">
        {[
          ["ID", sf.id, true],
          ["Minted", localDateTime(sf.timestamp_ms), false],
          ["Node", sf.node, false],
          ["Sequence", sf.sequence, false],
        ].map(([k, v, mono]) => (
          <div key={k} className="min-w-0 bg-kx-surface px-3 py-2.5">
            <dt className="kx-label">{k}</dt>
            <dd className={`mt-0.5 truncate text-sm text-kx-ink ${mono ? "font-mono" : "kx-num"}`}>
              {v}
            </dd>
          </div>
        ))}
      </dl>
    </Panel>
  );
}

export default function LinkDetail() {
  const { code } = useParams();
  const [link, setLink] = useState(null);
  const [sf, setSf] = useState(null);
  const [range, setRange] = useState("7d");
  const [gran, setGran] = useState("");
  const [ts, setTs] = useState(null);
  const [summary, setSummary] = useState(null);
  const [seriesErr, setSeriesErr] = useState("");
  const [dim, setDim] = useState("referrer");
  const [rows, setRows] = useState(null);
  const [rowsErr, setRowsErr] = useState("");
  const [linkErr, setLinkErr] = useState("");
  const c = useChartColors();

  useEffect(() => {
    let alive = true;
    api
      .get(`/links/${code}`)
      .then(({ data }) => {
        if (!alive) return;
        setLink(data.link);
        setSf(data.snowflake);
        setLinkErr("");
      })
      .catch((e) => {
        if (!alive) return;
        const m = apiError(e, "Could not load link");
        setLinkErr(m);
        toast.error(m);
      });
    return () => {
      alive = false;
    };
  }, [code]);

  const loadSeries = useCallback(async () => {
    try {
      const g = gran ? `&gran=${gran}` : "";
      const [t, sm] = await Promise.all([
        api.get(`/analytics/${code}/timeseries?range=${range}${g}`),
        api.get(`/analytics/${code}/summary`),
      ]);
      setTs(t.data);
      setSummary(sm.data);
      setSeriesErr("");
    } catch (e) {
      setSeriesErr(apiError(e, "Could not load analytics"));
    }
  }, [code, range, gran]);

  usePolling(loadSeries, 8000);

  const loadRows = useCallback(async () => {
    try {
      const { data } = await api.get(
        `/analytics/${code}/breakdown?dim=${dim}&range=${range}&limit=12`,
      );
      setRows(data.rows || []);
      setRowsErr("");
    } catch (e) {
      setRowsErr(apiError(e, "Could not load breakdown"));
    }
  }, [code, dim, range]);

  usePolling(loadRows, 8000);

  const series = useMemo(() => ts?.points || [], [ts]);
  const queryUs = ts?.query_us || 0;
  const xFmt = useMemo(() => seriesFormatter(ts), [ts]);
  const tipFmt = useMemo(() => seriesFormatter(ts, { full: true }), [ts]);
  const windowTotal = useMemo(() => series.reduce((n, p) => n + (p.count || 0), 0), [series]);
  // Leave room for the last x label, which is centred on the right edge.
  const rightPad = useMemo(() => {
    const last = series.length ? xFmt(series[series.length - 1].t) : "";
    return Math.max(16, Math.ceil(last.length * 3.6) + 6);
  }, [series, xFmt]);

  const rangeLabel = RANGE_LABEL[range];
  const dimTitle = DIMS.find((d) => d.v === dim)?.t ?? dim;
  const keyLabel = DIM_LABEL[dim] ?? ((k) => k || "Unknown");
  const topRows = (rows || []).slice(0, 8);
  const topMax = Math.max(1, ...topRows.map((r) => r.count));
  const host = prettyUrl(code)
    .replace(/^https?:\/\//, "")
    .slice(0, -(code.length + 1));
  const expired = isPast(link?.expires_at);
  const s = summary;

  const kpis = [
    {
      testid: "detail-total",
      label: "Total clicks",
      value: s ? compact(s.total) : "—",
      sub: s ? (s.first_seen ? `Since ${longDate(s.first_seen)}` : "No clicks yet") : "",
    },
    {
      testid: "detail-24h",
      label: "Last 24 hours",
      value: s ? compact(s.last_24h) : "—",
      sub: s ? `${share(s.last_24h, s.total)} of all clicks` : "",
    },
    {
      testid: "detail-1h",
      label: "Last hour",
      value: s ? compact(s.last_1h) : "—",
      sub: s ? (s.last_seen ? `Last click ${ago(s.last_seen)}` : "No clicks yet") : "",
      subTestid: "detail-last",
    },
    {
      testid: "detail-bots",
      label: "Bot traffic",
      value: s ? share(s.bots, s.total) : "—",
      sub: s ? `${fmt.format(s.bots)} of ${fmt.format(s.total)} clicks` : "",
    },
  ];

  return (
    <div className="space-y-4" data-testid="link-detail-page">
      <Link
        to="/app/links"
        className="inline-flex items-center gap-1.5 text-sm text-kx-muted hover:text-kx-ink"
        data-testid="back-to-links"
      >
        <ArrowLeft size={14} aria-hidden="true" /> Links
      </Link>

      <PageHeader
        title={
          <span className="break-all font-mono">
            <span className="text-kx-muted">{host}/</span>
            {code}
          </span>
        }
        sub={
          link ? (
            <span className="flex flex-col gap-2">
              <a
                href={link.long_url}
                target="_blank"
                rel="noreferrer"
                className="inline-flex min-w-0 max-w-full items-center gap-1.5 hover:text-kx-ink sm:max-w-xl"
              >
                <span className="flex-none">Redirects to</span>
                <span className="truncate font-mono text-kx-ink2">{link.long_url}</span>
              </a>
              <span className="flex flex-wrap gap-1.5">
                {expired ? (
                  <Chip tone="warning">Expired</Chip>
                ) : link.active ? (
                  <Chip tone="positive">Active</Chip>
                ) : (
                  <Chip>Disabled</Chip>
                )}
                <Chip>{link.custom ? "Custom alias" : "Generated code"}</Chip>
                <Chip>{link.redirect_type} redirect</Chip>
              </span>
            </span>
          ) : linkErr ? null : (
            "Loading link…"
          )
        }
        right={
          <>
            <a
              href={shortUrl(code)}
              target="_blank"
              rel="noreferrer"
              data-testid="detail-open"
              className="kx-btn inline-flex h-9 items-center justify-center gap-2 whitespace-nowrap border border-kx-line2 bg-kx-surface px-4 text-kx-ink hover:bg-kx-hover"
            >
              <ExternalLink size={14} aria-hidden="true" /> Open
            </a>
            <Button
              data-testid="detail-copy"
              onClick={() => copyText(prettyUrl(code), { success: "Short link copied" })}
            >
              <Copy size={14} aria-hidden="true" /> Copy link
            </Button>
          </>
        }
      />

      {linkErr && <ErrorNote testid="detail-link-error">{linkErr}</ErrorNote>}
      {seriesErr && (
        <ErrorNote testid="detail-series-error" onRetry={loadSeries}>
          {seriesErr} — showing the last good data
        </ErrorNote>
      )}

      <StatStrip items={kpis} />

      <Panel
        testid="detail-timeseries"
        title="Clicks"
        sub={ts ? `${rangeLabel} · ${fmt.format(windowTotal)} clicks` : rangeLabel}
        right={
          <div className="flex flex-wrap justify-end gap-2">
            <Segmented
              label="Time range"
              options={RANGES}
              value={range}
              onChange={setRange}
              size="sm"
              testidPrefix="detail-range-"
            />
            <Segmented
              label="Granularity"
              options={GRANS}
              value={gran}
              onChange={setGran}
              size="sm"
              testidPrefix="detail-gran-"
            />
          </div>
        }
      >
        <div className="h-[260px]">
          {!ts ? (
            seriesErr ? (
              <Empty className="h-full">Analytics are unavailable right now</Empty>
            ) : (
              <Skeleton className="h-full w-full" />
            )
          ) : series.length === 0 ? (
            <Empty className="h-full">No clicks in this window</Empty>
          ) : (
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={series} margin={{ top: 8, right: rightPad, left: 0, bottom: 0 }}>
                <CartesianGrid stroke={c.grid} vertical={false} />
                <XAxis
                  dataKey="t"
                  tickFormatter={xFmt}
                  tick={{ ...TICK, fill: c.muted }}
                  tickLine={false}
                  axisLine={false}
                  tickMargin={8}
                  minTickGap={40}
                />
                <YAxis
                  width={40}
                  tick={{ ...TICK, fill: c.muted }}
                  tickLine={false}
                  axisLine={false}
                  tickCount={5}
                  allowDecimals={false}
                  tickFormatter={compact}
                />
                <Tooltip
                  content={<ChartTip labelFormat={tipFmt} />}
                  cursor={{ stroke: c.line2, strokeWidth: 1 }}
                />
                <Area
                  type="linear"
                  dataKey="count"
                  name="Clicks"
                  stroke={c.accent}
                  strokeWidth={2}
                  fill={c.accent}
                  fillOpacity={0.08}
                  dot={false}
                  activeDot={{ r: 3, fill: c.accent, stroke: c.surface, strokeWidth: 2 }}
                  isAnimationActive={false}
                />
              </AreaChart>
            </ResponsiveContainer>
          )}
        </div>
        {ts && (
          <p className="mt-3 text-xs text-kx-muted">
            Served from pre-aggregated rollups in{" "}
            <span className="kx-num">{(queryUs / 1000).toFixed(3)} ms</span>
          </p>
        )}
      </Panel>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-[minmax(0,1fr)_300px]">
        <Panel testid="detail-breakdown" title={dimTitle} sub={rangeLabel} className="min-w-0">
          <div className="-mx-1 mb-4 overflow-x-auto px-1">
            <Segmented
              label="Breakdown"
              options={DIMS}
              value={dim}
              onChange={setDim}
              testidPrefix="dim-"
              size="sm"
            />
          </div>
          {rows === null ? (
            rowsErr ? (
              <Empty>{rowsErr}</Empty>
            ) : (
              <Skeleton className="h-[240px] w-full" />
            )
          ) : rows.length === 0 ? (
            <Empty>No clicks in this window</Empty>
          ) : (
            <ul data-testid="detail-toplist" className="space-y-3">
              {topRows.map((r) => (
                <li key={r.key}>
                  <div className="flex items-baseline justify-between gap-3 text-sm">
                    <span className="min-w-0 truncate text-kx-ink">{keyLabel(r.key)}</span>
                    <span className="kx-num flex-none text-xs text-kx-muted">
                      <span className="text-kx-ink2">{fmt.format(r.count)}</span> ·{" "}
                      {r.pct.toFixed(1)}%
                    </span>
                  </div>
                  <div className="mt-1.5">
                    <MiniBar pct={(r.count / topMax) * 100} />
                  </div>
                </li>
              ))}
            </ul>
          )}
        </Panel>

        <Panel testid="detail-qr" title="QR code" sub="Scans to the short link">
          <img
            alt={`QR code for ${prettyUrl(code)}`}
            className="mx-auto w-full max-w-[220px] rounded-control border border-kx-line"
            src={qrUrl(code)}
          />
          <dl className="mt-4 space-y-0 text-sm">
            {[
              ["Created", link?.created_at ? longDate(link.created_at) : "—"],
              ["Expires", link ? (link.expires_at ? longDate(link.expires_at) : "Never") : "—"],
              [
                "Redirect",
                link
                  ? `${link.redirect_type} · ${link.redirect_type === 301 ? "permanent" : "temporary"}`
                  : "—",
              ],
            ].map(([k, v]) => (
              <div
                key={k}
                className="flex justify-between gap-3 border-b border-kx-line py-2 last:border-0"
              >
                <dt className="text-kx-muted">{k}</dt>
                <dd className={`kx-num ${v === "—" ? "text-kx-muted" : "text-kx-ink"}`}>{v}</dd>
              </div>
            ))}
          </dl>
          <a
            href={qrUrl(code)}
            download={`${code}-qr.png`}
            target="_blank"
            rel="noreferrer"
            className="kx-btn mt-3 inline-flex h-8 w-full items-center justify-center gap-2 whitespace-nowrap text-kx-muted hover:bg-kx-hover hover:text-kx-ink"
          >
            <Download size={14} aria-hidden="true" /> Download PNG
          </a>
        </Panel>
      </div>

      <section className="space-y-4 pt-2" aria-labelledby="under-the-hood">
        <div>
          <h2 id="under-the-hood" className="text-base font-semibold text-kx-ink">
            Under the hood
          </h2>
          <p className="mt-0.5 text-sm text-kx-muted">How this link&apos;s numbers are produced.</p>
        </div>
        <div className={`grid grid-cols-1 gap-4 ${sf ? "lg:grid-cols-2" : ""}`}>
          <Race code={code} />
          {sf && <IdAnatomy sf={sf} />}
        </div>
      </section>
    </div>
  );
}
