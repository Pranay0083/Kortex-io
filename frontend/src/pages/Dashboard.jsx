import { useCallback, useMemo, useRef, useState } from "react";
import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { api, apiError, compact, fmt, ago } from "@/lib/api";
import { clockMs, seriesFormatter } from "@/lib/time";
import { usePolling } from "@/hooks/usePolling";
import { useChartColors } from "@/context/ThemeContext";
import {
  Panel,
  Chip,
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

// A click counts as "live" if the newest one arrived within this window.
const LIVE_MS = 60_000;

const TICK = { fontSize: 11, fontFamily: '"IBM Plex Sans", sans-serif' };

// Neutral steps after the accent for multi-part shares (device mix).
const SHARE_SHADES = ["bg-kx-accent", "bg-kx-muted", "bg-kx-faint", "bg-kx-line2", "bg-kx-hover"];

const longDate = (ms) =>
  new Date(ms).toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" });

const share = (part, total) => (total ? `${((part / total) * 100).toFixed(1)}%` : "0%");

const capitalize = (s) => (s ? s.charAt(0).toUpperCase() + s.slice(1) : "Unknown");

const referrerName = (k) => (!k || k === "direct" ? "Direct" : k);

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

/** Ranked list with a thin accent bar scaled to the top item. */
function RankList({ rows, label = (k) => k, limit = 7 }) {
  const top = rows.slice(0, limit);
  const max = Math.max(1, ...top.map((r) => r.count));
  return (
    <ul className="space-y-3">
      {top.map((r) => (
        <li key={r.key}>
          <div className="flex items-baseline justify-between gap-3 text-sm">
            <span className="min-w-0 truncate text-kx-ink">{label(r.key)}</span>
            <span className="kx-num flex-none text-xs text-kx-muted">
              <span className="text-kx-ink2">{fmt.format(r.count)}</span> · {r.pct.toFixed(1)}%
            </span>
          </div>
          <div className="mt-1.5">
            <MiniBar pct={(r.count / max) * 100} />
          </div>
        </li>
      ))}
    </ul>
  );
}

function ShareTable({ rows, shades = true }) {
  return (
    <table className="w-full text-sm">
      <tbody>
        {rows.map((r, i) => (
          <tr key={r.key} className="border-b border-kx-line last:border-0">
            <td className="py-1.5 pr-2">
              <span className="flex items-center gap-2 text-kx-ink">
                {shades && (
                  <span
                    aria-hidden="true"
                    className={`h-2 w-2 flex-none rounded-full ${SHARE_SHADES[Math.min(i, SHARE_SHADES.length - 1)]}`}
                  />
                )}
                <span className="truncate">{capitalize(r.key)}</span>
              </span>
            </td>
            <td className="kx-num py-1.5 pr-3 text-right text-kx-ink2">{fmt.format(r.count)}</td>
            <td className="kx-num w-14 py-1.5 text-right text-kx-muted">{r.pct.toFixed(1)}%</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function DeviceMix({ device, browser }) {
  return (
    <div>
      <div
        className="flex h-2 w-full gap-0.5 overflow-hidden rounded-full"
        role="img"
        aria-label={device.map((d) => `${d.key} ${d.pct.toFixed(1)}%`).join(", ")}
      >
        {device.map((d, i) => (
          <div
            key={d.key}
            className={`h-full ${SHARE_SHADES[Math.min(i, SHARE_SHADES.length - 1)]}`}
            style={{ width: `${d.pct}%` }}
          />
        ))}
      </div>
      <div className="mt-3">
        <ShareTable rows={device} />
      </div>
      {browser?.length > 0 && (
        <>
          <div className="kx-label mt-5">Browsers</div>
          <div className="mt-1">
            <ShareTable rows={browser.slice(0, 4)} shades={false} />
          </div>
        </>
      )}
    </div>
  );
}

function ClickStream() {
  const [events, setEvents] = useState([]);
  const [err, setErr] = useState("");
  const [loaded, setLoaded] = useState(false);
  const [checkedAt, setCheckedAt] = useState(0);
  const [fresh, setFresh] = useState(() => new Set());
  const seen = useRef(new Set());

  const tick = useCallback(async () => {
    try {
      // Authenticated: only this tenant's events, no client IPs.
      const { data } = await api.get("/stream/recent?limit=24");
      const dup = {};
      const list = (data.events || []).map((e) => {
        const base = `${e.ts}-${e.code}`;
        dup[base] = (dup[base] || 0) + 1;
        return { ...e, _k: `${base}-${dup[base]}` };
      });
      // Rows we haven't shown before get the flash animation.
      const isFirst = seen.current.size === 0;
      setFresh(
        new Set(isFirst ? [] : list.filter((e) => !seen.current.has(e._k)).map((e) => e._k)),
      );
      seen.current = new Set(list.map((e) => e._k));
      setEvents(list);
      setErr("");
    } catch (e) {
      setErr(apiError(e, "Live clicks are unavailable"));
    }
    setCheckedAt(Date.now());
    setLoaded(true);
  }, []);

  usePolling(tick, 2000);

  const newest = events.length ? Math.max(...events.map((e) => e.ts)) : 0;
  const live = !err && newest > 0 && checkedAt - newest < LIVE_MS;

  return (
    <Panel
      testid="click-stream-panel"
      title="Live clicks"
      sub="Updates every 2 seconds"
      right={
        err ? (
          <Chip tone="negative">Offline</Chip>
        ) : (
          <Chip tone={live ? "positive" : "neutral"}>
            <span
              aria-hidden="true"
              className={`h-1.5 w-1.5 rounded-full ${live ? "animate-kx-pulse bg-kx-positive" : "bg-kx-faint"}`}
            />
            {live ? "Live" : "Idle"}
          </Chip>
        )
      }
      pad="p-0"
      className="overflow-hidden"
    >
      {err && (
        <div className="px-5 pt-4">
          <ErrorNote testid="stream-error">{err} — retrying</ErrorNote>
        </div>
      )}
      {!loaded ? (
        <div className="px-5 py-4" aria-busy="true">
          <Skeleton className="h-5 w-full" />
        </div>
      ) : events.length === 0 ? (
        <p className="px-5 py-4 text-sm text-kx-muted">
          No recent clicks — open one of your short links to see it here.
        </p>
      ) : (
        <div className="max-h-[320px] overflow-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-kx-line text-left text-xs text-kx-muted">
                <th className="px-5 py-2 font-medium">Time</th>
                <th className="px-2 py-2 font-medium">Link</th>
                <th className="px-2 py-2 font-medium">Referrer</th>
                <th className="hidden px-5 py-2 font-medium md:table-cell">Device</th>
              </tr>
            </thead>
            <tbody>
              {events.map((e) => (
                <tr
                  key={e._k}
                  className={`border-b border-kx-line last:border-0 ${fresh.has(e._k) ? "kx-row-new" : ""}`}
                >
                  <td className="kx-num whitespace-nowrap px-5 py-2 text-kx-muted">
                    {clockMs(e.ts)}
                  </td>
                  <td className="px-2 py-2 font-mono text-kx-ink">/{e.code}</td>
                  <td className="px-2 py-2 text-kx-ink2">{referrerName(e.referrer)}</td>
                  <td className="hidden px-5 py-2 text-kx-ink2 md:table-cell">
                    {e.device ? capitalize(e.device) : "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Panel>
  );
}

const PanelPlaceholder = ({ err }) =>
  err ? <Empty>Unavailable right now</Empty> : <Skeleton className="h-[180px] w-full" />;

export default function Dashboard() {
  const [range, setRange] = useState("24h");
  const [ov, setOv] = useState(null);
  const [ts, setTs] = useState(null);
  const [err, setErr] = useState("");
  const c = useChartColors();

  const load = useCallback(async () => {
    try {
      const [o, t] = await Promise.all([
        api.get(`/analytics/overview?range=${range}`),
        api.get(`/analytics/all/timeseries?range=${range}`),
      ]);
      setOv(o.data);
      setTs(t.data);
      setErr("");
    } catch (e) {
      setErr(apiError(e, "Could not load analytics"));
    }
  }, [range]);

  usePolling(load, 8000);

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
  const s = ov?.summary;
  const rangeLabel = RANGE_LABEL[range];
  const n = ov?.links;

  const kpis = [
    {
      testid: "stat-total-clicks",
      label: "Total clicks",
      value: s ? compact(s.total) : "—",
      sub: s ? (s.first_seen ? `Since ${longDate(s.first_seen)}` : "No clicks yet") : "",
      subTestid: "stat-first-seen",
    },
    {
      testid: "stat-clicks-24h",
      label: "Last 24 hours",
      value: s ? compact(s.last_24h) : "—",
      sub: s ? `${share(s.last_24h, s.total)} of all clicks` : "",
    },
    {
      testid: "stat-clicks-1h",
      label: "Last hour",
      value: s ? compact(s.last_1h) : "—",
      sub: s ? (s.last_seen ? `Last click ${ago(s.last_seen)}` : "No clicks yet") : "",
      subTestid: "stat-last-seen",
    },
    {
      testid: "stat-bots",
      label: "Bot traffic",
      value: s ? share(s.bots, s.total) : "—",
      sub: s ? `${fmt.format(s.bots)} of ${fmt.format(s.total)} clicks` : "",
    },
  ];

  return (
    <div className="space-y-4" data-testid="dashboard-page">
      <PageHeader
        title={`Traffic across ${n ?? "—"} link${n === 1 ? "" : "s"}`}
        sub={
          ts ? (
            <>
              Served from pre-aggregated rollups in{" "}
              <span data-testid="stat-query-time" className="kx-num">
                {(queryUs / 1000).toFixed(2)} ms
              </span>{" "}
              · refreshes every 8 seconds
            </>
          ) : (
            "Loading your analytics…"
          )
        }
        right={
          <Segmented
            label="Time range"
            options={RANGES}
            value={range}
            onChange={setRange}
            testidPrefix="range-"
          />
        }
      />

      {err && (
        <ErrorNote testid="dashboard-error" onRetry={load}>
          {err}
          {ov ? " — showing the last good data" : ""}
        </ErrorNote>
      )}

      <StatStrip items={kpis} />

      <Panel
        testid="timeseries-panel"
        title="Clicks"
        sub={ts ? `${rangeLabel} · ${fmt.format(windowTotal)} clicks` : rangeLabel}
      >
        <div className="h-[280px]">
          {!ts ? (
            err ? (
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
      </Panel>

      <div className="grid gap-4 lg:grid-cols-2">
        <Panel testid="referrer-panel" title="Top referrers" sub={rangeLabel}>
          {!ov ? (
            <PanelPlaceholder err={err} />
          ) : !ov.referrer?.length ? (
            <Empty>No clicks in this window</Empty>
          ) : (
            <RankList rows={ov.referrer} label={referrerName} />
          )}
        </Panel>

        <Panel testid="device-panel" title="Devices" sub={rangeLabel}>
          {!ov ? (
            <PanelPlaceholder err={err} />
          ) : !ov.device?.length ? (
            <Empty>No clicks in this window</Empty>
          ) : (
            <DeviceMix device={ov.device} browser={ov.browser} />
          )}
        </Panel>
      </div>

      <ClickStream />
    </div>
  );
}
