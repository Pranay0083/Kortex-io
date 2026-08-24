import { useCallback, useEffect, useRef, useState } from "react";
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { toast } from "sonner";
import { Check, Play, Square, Waves } from "lucide-react";
import { api, apiError, compact, fmt } from "@/lib/api";
import { useChartColors } from "@/context/ThemeContext";
import { usePolling } from "@/hooks/usePolling";
import { Panel, Stat, Chip, Button, Empty, ChartTip, ErrorNote, PageHeader } from "@/components/kx";

const TARGETS = {
  rps: 21000,
  p99: 6,
  hit: 99.2,
  ingest: 41666,
};

const cap = (s) => (s ? s.charAt(0).toUpperCase() + s.slice(1) : s);
const KIND = { redirect: "load test", ingest: "event ingest" };

function Terminal({ lines }) {
  const ref = useRef();
  useEffect(() => {
    if (ref.current) ref.current.scrollTop = ref.current.scrollHeight;
  }, [lines]);
  return (
    <div
      ref={ref}
      data-testid="bench-terminal"
      className="max-h-[360px] overflow-y-auto rounded-control border border-kx-line bg-kx-card px-4 py-3 font-mono text-xs leading-relaxed text-kx-ink2"
    >
      {lines.length === 0 ? (
        <span className="font-sans text-sm text-kx-muted">Waiting for a run.</span>
      ) : (
        lines.map((l, i) => (
          <div key={i} className="whitespace-pre-wrap">
            {l}
          </div>
        ))
      )}
    </div>
  );
}

function Slider({ id, label, value, display, testid, onChange, ...range }) {
  return (
    <div>
      <div className="flex items-baseline justify-between gap-2">
        <label className="kx-label" htmlFor={id}>
          {label}
        </label>
        <span className="kx-num text-sm font-semibold text-kx-ink">{display}</span>
      </div>
      <input
        id={id}
        type="range"
        value={value}
        data-testid={testid}
        onChange={(e) => onChange(e.target.value)}
        className="mt-2 w-full"
        {...range}
      />
    </div>
  );
}

function TargetRow({ label, measured, target, format, unit, better }) {
  const has = measured > 0;
  const met = has && (better === "high" ? measured >= target : measured <= target);
  // The track is 0 → target; the fill is the true measured / target ratio (capped at the end).
  const pct = has ? Math.min(100, (measured / target) * 100) : 0;
  const over = has && better === "low" && measured > target;
  return (
    <div>
      <div className="flex items-baseline justify-between gap-3">
        <span className="text-sm text-kx-ink2">{label}</span>
        <span className="flex items-baseline gap-2">
          {met && (
            <span className="inline-flex items-center gap-1 text-xs font-medium text-kx-positive">
              <Check size={12} aria-hidden="true" /> Met
            </span>
          )}
          {over && <span className="text-xs font-medium text-kx-warning">Over target</span>}
          <span className={`kx-num text-sm font-semibold ${has ? "text-kx-ink" : "text-kx-muted"}`}>
            {has ? format(measured) : "—"}
            {has && <span className="ml-1 font-normal text-kx-muted">{unit}</span>}
          </span>
        </span>
      </div>
      <div className="mt-1.5 h-1.5 w-full overflow-hidden rounded-full bg-kx-hover">
        <div
          className={`h-full rounded-full transition-[width] duration-700 ${
            over ? "bg-kx-warning" : "bg-kx-accent"
          }`}
          style={{ width: `${pct}%` }}
        />
      </div>
      <div className="kx-num mt-1 flex justify-between text-xs text-kx-faint">
        <span>0</span>
        <span>
          Target {format(target)} {unit}
        </span>
      </div>
    </div>
  );
}

export default function Bench() {
  const [snap, setSnap] = useState(null);
  const [duration, setDuration] = useState(10);
  const [vus, setVus] = useState(64);
  const [events, setEvents] = useState(1000000);
  const [err, setErr] = useState("");
  const c = useChartColors();

  const refresh = useCallback(async () => {
    try {
      const { data } = await api.get("/bench/status");
      setSnap(data);
      setErr("");
      return data;
    } catch (e) {
      setErr(apiError(e, "Could not read benchmark status"));
      return null;
    }
  }, []);

  // Fast while a run is live, lazy otherwise; both pause in background tabs.
  usePolling(refresh, snap?.running ? 700 : 5000);

  const start = async (k) => {
    try {
      await api.post("/bench/start", {
        kind: k,
        duration_s: Number(duration),
        concurrency: Number(vus),
        events: Number(events),
      });
      toast.success(k === "ingest" ? "Ingest benchmark started" : "Load test started");
      refresh();
    } catch (e) {
      toast.error(apiError(e, "Could not start benchmark"));
    }
  };

  const stop = async () => {
    try {
      await api.post("/bench/stop");
      toast.message("Stopping…");
      refresh();
    } catch (e) {
      toast.error(apiError(e, "Could not stop benchmark"));
    }
  };

  const running = !!snap?.running;
  const pct = Math.min(100, (snap?.progress || 0) * 100);
  // Each figure comes from the latest run of its own kind (the current one if it
  // matches), so a load test doesn't blank the previous ingest result.
  const lt = snap?.kind === "redirect" ? snap : snap?.last_redirect;
  const ig = snap?.kind === "ingest" ? snap : snap?.last_ingest;
  const ran = (lt?.requests || 0) > 0;
  const ingested = (ig?.ingest_rate || 0) > 0;
  const ms = (v, d = 2) => (v ? v.toFixed(d) : "—");
  const tick = { fill: c.muted, fontSize: 11 };

  return (
    <div className="space-y-5" data-testid="bench-page">
      <PageHeader
        title="Benchmark"
        sub="Load-test the redirect path and the click pipeline. Runs hit 16 dedicated benchmark links, so customer and demo analytics stay untouched."
      />

      {err && <ErrorNote testid="bench-error">{err}</ErrorNote>}

      <Panel
        testid="bench-controls"
        title="Run configuration"
        right={
          <Chip tone={running ? "accent" : "neutral"}>
            <span
              className={`h-1.5 w-1.5 rounded-full ${
                running ? "animate-kx-pulse bg-kx-accent" : "bg-kx-faint"
              }`}
            />
            {running ? "Running" : "Idle"}
          </Chip>
        }
      >
        <div className="grid gap-5 sm:grid-cols-3">
          <Slider
            id="bench-duration"
            label="Duration"
            min="5"
            max="60"
            value={duration}
            display={`${duration} s`}
            testid="bench-duration"
            onChange={setDuration}
          />
          <Slider
            id="bench-vus"
            label="Virtual users"
            min="8"
            max="512"
            step="8"
            value={vus}
            display={fmt.format(Number(vus))}
            testid="bench-vus"
            onChange={setVus}
          />
          <Slider
            id="bench-events"
            label="Events to ingest"
            min="100000"
            max="5000000"
            step="100000"
            value={events}
            display={compact(Number(events))}
            testid="bench-events"
            onChange={setEvents}
          />
        </div>

        <div className="mt-5 flex flex-col gap-4 border-t border-kx-line pt-4 md:flex-row md:items-center">
          <div className="min-w-0 flex-1">
            <div className="flex items-center justify-between text-xs text-kx-muted">
              <span>
                {cap(snap?.phase || "idle")}
                {snap?.kind ? ` · ${KIND[snap.kind] || snap.kind}` : ""}
              </span>
              <span className="kx-num">{pct.toFixed(0)}%</span>
            </div>
            <div className="mt-1.5 h-1.5 w-full overflow-hidden rounded-full bg-kx-hover">
              <div
                className="h-full rounded-full bg-kx-accent transition-[width] duration-500"
                style={{ width: `${pct}%` }}
                data-testid="bench-progress"
              />
            </div>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button
              disabled={running}
              onClick={() => start("redirect")}
              data-testid="run-benchmark-btn"
            >
              <Play size={14} aria-hidden="true" /> Run load test
            </Button>
            <Button
              variant="secondary"
              disabled={running}
              onClick={() => start("ingest")}
              data-testid="run-ingest-btn"
            >
              <Waves size={14} aria-hidden="true" /> Ingest events
            </Button>
            <Button
              variant="danger"
              disabled={!running}
              onClick={stop}
              data-testid="stop-benchmark-btn"
            >
              <Square size={12} aria-hidden="true" /> Stop
            </Button>
          </div>
        </div>
      </Panel>

      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 xl:grid-cols-6">
        <Stat
          testid="bench-rps"
          label="Throughput"
          value={lt?.rps ? fmt.format(Math.round(lt.rps)) : "—"}
          tone={lt?.rps ? "default" : "muted"}
          unit={lt?.rps ? "req/s" : undefined}
          sub={`Peak ${lt?.peak_rps ? fmt.format(Math.round(lt.peak_rps)) : "—"}`}
        />
        <Stat
          testid="bench-p99"
          label="p99 latency"
          value={ms(lt?.p99_ms)}
          tone={lt?.p99_ms ? "default" : "muted"}
          unit={lt?.p99_ms ? "ms" : undefined}
          sub={`p50 ${ms(lt?.p50_ms)} ms`}
        />
        <Stat
          testid="bench-p95"
          label="p95 latency"
          value={ms(lt?.p95_ms)}
          tone={lt?.p95_ms ? "default" : "muted"}
          unit={lt?.p95_ms ? "ms" : undefined}
          sub={`Max ${ms(lt?.max_ms, 1)} ms`}
        />
        <Stat
          testid="bench-hitrate"
          label="Cache hit rate"
          value={lt?.cache_hit_rate ? lt.cache_hit_rate.toFixed(2) : "—"}
          tone={lt?.cache_hit_rate ? "default" : "muted"}
          unit={lt?.cache_hit_rate ? "%" : undefined}
          sub={ran ? `${compact(lt.cache_hits)} hits` : "No run yet"}
        />
        <Stat
          testid="bench-requests"
          label="Requests"
          value={ran ? compact(lt.requests) : "—"}
          tone={ran ? "default" : "muted"}
          sub={
            ran
              ? `${fmt.format(lt.errors || 0)} ${lt.errors === 1 ? "error" : "errors"}`
              : "No run yet"
          }
        />
        <Stat
          testid="bench-ingest"
          label="Ingest rate"
          value={ingested ? compact(ig.ingest_rate) : "—"}
          tone={ingested ? "default" : "muted"}
          unit={ingested ? "evt/s" : undefined}
          sub={ingested ? `${compact(ig.events_stored)} rows stored` : "No run yet"}
        />
      </div>

      <div className="space-y-5">
        <Panel
          testid="bench-chart"
          title="Throughput and p99 latency"
          sub="Sampled every 500 ms during the run"
        >
          {!snap?.series?.length ? (
            <Empty>Run a load test to see throughput over time.</Empty>
          ) : (
            <>
              <div className="mb-3 flex items-center gap-4 text-xs text-kx-muted">
                <span className="inline-flex items-center gap-1.5">
                  <span className="h-0.5 w-3 rounded-full bg-kx-accent" /> Requests/s
                </span>
                <span className="inline-flex items-center gap-1.5">
                  <span className="h-0.5 w-3 rounded-full bg-kx-ink2" /> p99 ms
                </span>
              </div>
              <div className="h-[240px]">
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={snap.series} margin={{ top: 6, right: 0, left: -12, bottom: 0 }}>
                    <CartesianGrid stroke={c.grid} vertical={false} />
                    <XAxis
                      dataKey="t"
                      tickFormatter={(v) => (v % 1 ? `${v.toFixed(1)}s` : `${v}s`)}
                      tick={tick}
                      tickLine={false}
                      axisLine={{ stroke: c.line }}
                      minTickGap={24}
                    />
                    <YAxis
                      yAxisId="l"
                      tick={tick}
                      tickLine={false}
                      axisLine={false}
                      tickFormatter={compact}
                    />
                    <YAxis
                      yAxisId="r"
                      orientation="right"
                      tick={tick}
                      tickLine={false}
                      axisLine={false}
                      width={36}
                    />
                    <Tooltip
                      content={<ChartTip labelFormat={(v) => `${Number(v).toFixed(1)} s`} />}
                      cursor={{ stroke: c.line2 }}
                    />
                    <Line
                      yAxisId="l"
                      type="linear"
                      dataKey="rps"
                      name="Requests/s"
                      stroke={c.accent}
                      strokeWidth={2}
                      dot={false}
                      isAnimationActive={false}
                    />
                    <Line
                      yAxisId="r"
                      type="linear"
                      dataKey="p99"
                      name="p99 ms"
                      stroke={c.ink2}
                      strokeWidth={1.25}
                      dot={false}
                      isAnimationActive={false}
                    />
                  </LineChart>
                </ResponsiveContainer>
              </div>
            </>
          )}
        </Panel>

        <Panel testid="bench-vs-target" title="Measured vs design target">
          <div className="grid gap-x-8 gap-y-5 sm:grid-cols-2 xl:grid-cols-4">
            <TargetRow
              label="Redirect throughput"
              measured={lt?.rps || 0}
              target={TARGETS.rps}
              format={(v) => fmt.format(Math.round(v))}
              unit="req/s"
              better="high"
            />
            <TargetRow
              label="p99 latency"
              measured={lt?.p99_ms || 0}
              target={TARGETS.p99}
              format={(v) => v.toFixed(2)}
              unit="ms"
              better="low"
            />
            <TargetRow
              label="Cache hit rate"
              measured={lt?.cache_hit_rate || 0}
              target={TARGETS.hit}
              format={(v) => v.toFixed(1)}
              unit="%"
              better="high"
            />
            <TargetRow
              label="Ingest rate"
              measured={ig?.ingest_rate || 0}
              target={TARGETS.ingest}
              format={(v) => compact(v)}
              unit="evt/s"
              better="high"
            />
          </div>
          <p className="mt-5 border-t border-kx-line pt-3 text-xs leading-relaxed text-kx-muted">
            Targets assume a dedicated node with the load generator on separate hardware. Here the
            runner shares CPU with the server it is testing, so p99 suffers most once the cores (or
            a container CPU quota) are saturated.
          </p>
        </Panel>
      </div>

      <Panel
        testid="bench-log-panel"
        title="Runner output"
        sub="k6-style summary written by the Go runner"
      >
        <Terminal lines={snap?.log || []} />
      </Panel>
    </div>
  );
}
