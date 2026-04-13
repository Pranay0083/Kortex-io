import { useEffect, useRef, useState } from "react";
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
import { Gauge, Play, Square, Waves } from "lucide-react";
import { api, apiError, compact, fmt } from "@/lib/api";
import { Panel, Stat, Chip, Button, Empty, ChartTip } from "@/components/kx";

const TARGETS = {
  rps: 21000,
  p99: 6,
  hit: 99.2,
  ingest: 41666,
};

function Terminal({ lines }) {
  const ref = useRef();
  useEffect(() => {
    if (ref.current) ref.current.scrollTop = ref.current.scrollHeight;
  }, [lines]);
  return (
    <div
      ref={ref}
      data-testid="bench-terminal"
      className="kx-scanlines relative h-[240px] overflow-y-auto bg-black p-4 font-mono text-[11px] leading-relaxed text-kx-green"
    >
      {lines.length === 0 ? (
        <span className="text-zinc-700">// waiting for a run…</span>
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

export default function Bench() {
  const [snap, setSnap] = useState(null);
  const [duration, setDuration] = useState(10);
  const [vus, setVus] = useState(64);
  const [events, setEvents] = useState(1000000);
  const [kind, setKind] = useState("redirect");
  const poll = useRef();

  const refresh = async () => {
    try {
      const { data } = await api.get("/bench/status");
      setSnap(data);
      return data;
    } catch {
      return null;
    }
  };

  useEffect(() => {
    refresh();
    poll.current = setInterval(refresh, 700);
    return () => clearInterval(poll.current);
  }, []);

  const start = async (k) => {
    setKind(k);
    try {
      await api.post("/bench/start", {
        kind: k,
        duration_s: Number(duration),
        concurrency: Number(vus),
        events: Number(events),
      });
      toast.success(k === "ingest" ? "Ingest benchmark started" : "Load test started");
    } catch (e) {
      toast.error(apiError(e));
    }
  };

  const stop = async () => {
    await api.post("/bench/stop");
    toast.message("Stopping…");
  };

  const running = snap?.running;
  const pct = Math.min(100, (snap?.progress || 0) * 100);

  return (
    <div className="space-y-4" data-testid="bench-page">
      <div>
        <div className="kx-label">Console / Benchmark</div>
        <h1 className="mt-1.5 font-display text-3xl font-black tracking-tighter lg:text-4xl">
          Load generator
        </h1>
        <p className="mt-3 max-w-3xl text-sm leading-relaxed text-zinc-400">
          A k6-style runner living inside the Go process. It opens real keep-alive HTTP connections to
          the loopback redirect endpoint, so every request pays for routing, Redis, expiry checks and
          the async event emit. The load generator shares CPU with the server it is testing — that
          ceiling is honest, not tuned.
        </p>
      </div>

      <Panel testid="bench-controls" title="run configuration">
        <div className="grid gap-4 lg:grid-cols-4">
          <div>
            <span className="kx-label">duration (s)</span>
            <input
              type="range"
              min="5"
              max="60"
              value={duration}
              data-testid="bench-duration"
              onChange={(e) => setDuration(e.target.value)}
              className="mt-3 w-full accent-kx-cyan"
            />
            <div className="kx-num mt-1 text-lg font-bold">{duration}s</div>
          </div>
          <div>
            <span className="kx-label">virtual users</span>
            <input
              type="range"
              min="8"
              max="512"
              step="8"
              value={vus}
              data-testid="bench-vus"
              onChange={(e) => setVus(e.target.value)}
              className="mt-3 w-full accent-kx-cyan"
            />
            <div className="kx-num mt-1 text-lg font-bold">{vus} VUs</div>
          </div>
          <div>
            <span className="kx-label">ingest events</span>
            <input
              type="range"
              min="100000"
              max="5000000"
              step="100000"
              value={events}
              data-testid="bench-events"
              onChange={(e) => setEvents(e.target.value)}
              className="mt-3 w-full accent-kx-yellow"
            />
            <div className="kx-num mt-1 text-lg font-bold">{compact(Number(events))}</div>
          </div>
          <div className="flex flex-col justify-end gap-2">
            <Button
              variant="solid"
              disabled={running}
              onClick={() => start("redirect")}
              data-testid="run-benchmark-btn"
              className="py-3"
            >
              <Play size={13} strokeWidth={2.5} /> Run redirect load test
            </Button>
            <div className="flex gap-2">
              <Button
                variant="ghost"
                disabled={running}
                onClick={() => start("ingest")}
                data-testid="run-ingest-btn"
                className="flex-1"
              >
                <Waves size={12} /> Ingest
              </Button>
              <Button variant="danger" disabled={!running} onClick={stop} data-testid="stop-benchmark-btn">
                <Square size={12} />
              </Button>
            </div>
          </div>
        </div>

        <div className="mt-5">
          <div className="flex items-center justify-between font-mono text-[10px] uppercase tracking-widest text-zinc-500">
            <span>
              {snap?.phase || "idle"} · {snap?.kind || "—"}
            </span>
            <span>{pct.toFixed(0)}%</span>
          </div>
          <div className="mt-2 h-1.5 w-full overflow-hidden bg-kx-line">
            <div
              className="h-full bg-kx-cyan transition-[width] duration-500"
              style={{ width: `${pct}%` }}
              data-testid="bench-progress"
            />
          </div>
        </div>
      </Panel>

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-6">
        <Stat
          testid="bench-rps"
          label="throughput"
          value={snap?.rps ? fmt.format(Math.round(snap.rps)) : "—"}
          unit="req/s"
          sub={`peak ${snap?.peak_rps ? fmt.format(Math.round(snap.peak_rps)) : "—"}`}
          accent="#00E5FF"
        />
        <Stat
          testid="bench-p99"
          label="p99 latency"
          value={snap?.p99_ms ? snap.p99_ms.toFixed(2) : "—"}
          unit="ms"
          sub={`p50 ${snap?.p50_ms ? snap.p50_ms.toFixed(2) : "—"}ms`}
          accent="#00FF66"
        />
        <Stat
          testid="bench-p95"
          label="p95 latency"
          value={snap?.p95_ms ? snap.p95_ms.toFixed(2) : "—"}
          unit="ms"
          sub={`max ${snap?.max_ms ? snap.max_ms.toFixed(1) : "—"}ms`}
        />
        <Stat
          testid="bench-hitrate"
          label="cache hit rate"
          value={snap?.cache_hit_rate ? snap.cache_hit_rate.toFixed(2) : "—"}
          unit="%"
          sub={snap ? `${compact(snap.cache_hits)} hits` : ""}
          accent="#FFD60A"
        />
        <Stat
          testid="bench-requests"
          label="requests"
          value={snap ? compact(snap.requests) : "—"}
          sub={snap ? `${snap.errors} errors` : ""}
        />
        <Stat
          testid="bench-ingest"
          label="ingest rate"
          value={snap?.ingest_rate ? compact(snap.ingest_rate) : "—"}
          unit="evt/s"
          sub={snap ? `${compact(snap.events_stored)} rows` : ""}
          accent="#8A2BE2"
        />
      </div>

      <div className="grid gap-3 lg:grid-cols-[1.4fr_1fr]">
        <Panel
          testid="bench-chart"
          title="throughput & p99 over time"
          sub="sampled every 500ms during the run"
          right={
            <Chip tone={running ? "green" : "zinc"}>
              <span className={`h-1.5 w-1.5 ${running ? "animate-kx-pulse bg-kx-green" : "bg-zinc-600"}`} />
              {running ? "running" : "idle"}
            </Chip>
          }
        >
          <div className="h-[240px]">
            {!snap?.series?.length ? (
              <Empty>run a load test to plot throughput</Empty>
            ) : (
              <ResponsiveContainer width="100%" height="100%">
                <LineChart data={snap.series} margin={{ top: 6, right: 8, left: -18, bottom: 0 }}>
                  <CartesianGrid stroke="#1b1b1b" vertical={false} />
                  <XAxis
                    dataKey="t"
                    tickFormatter={(v) => `${v.toFixed(1)}s`}
                    tick={{ fill: "#52525b", fontSize: 10, fontFamily: "JetBrains Mono" }}
                    tickLine={false}
                    axisLine={{ stroke: "#222" }}
                    minTickGap={24}
                  />
                  <YAxis
                    yAxisId="l"
                    tick={{ fill: "#52525b", fontSize: 10, fontFamily: "JetBrains Mono" }}
                    tickLine={false}
                    axisLine={false}
                    tickFormatter={compact}
                  />
                  <YAxis
                    yAxisId="r"
                    orientation="right"
                    tick={{ fill: "#52525b", fontSize: 10, fontFamily: "JetBrains Mono" }}
                    tickLine={false}
                    axisLine={false}
                  />
                  <Tooltip content={<ChartTip />} />
                  <Line
                    yAxisId="l"
                    type="linear"
                    dataKey="rps"
                    name="req/s"
                    stroke="#00E5FF"
                    strokeWidth={1.8}
                    dot={false}
                    isAnimationActive={false}
                  />
                  <Line
                    yAxisId="r"
                    type="linear"
                    dataKey="p99"
                    name="p99 ms"
                    stroke="#FF3B30"
                    strokeWidth={1.4}
                    dot={false}
                    isAnimationActive={false}
                  />
                </LineChart>
              </ResponsiveContainer>
            )}
          </div>
        </Panel>

        <Panel testid="bench-vs-target" title="measured vs design target">
          <div className="space-y-4">
            {[
              {
                l: "redirect throughput",
                m: snap?.rps || 0,
                t: TARGETS.rps,
                f: (v) => fmt.format(Math.round(v)),
                u: "req/s",
                better: "high",
              },
              {
                l: "p99 latency",
                m: snap?.p99_ms || 0,
                t: TARGETS.p99,
                f: (v) => v.toFixed(2),
                u: "ms",
                better: "low",
              },
              {
                l: "cache hit rate",
                m: snap?.cache_hit_rate || 0,
                t: TARGETS.hit,
                f: (v) => v.toFixed(2),
                u: "%",
                better: "high",
              },
              {
                l: "ingest rate",
                m: snap?.ingest_rate || 0,
                t: TARGETS.ingest,
                f: (v) => compact(v),
                u: "evt/s",
                better: "high",
              },
            ].map((r) => {
              const ratio = r.better === "high" ? r.m / r.t : r.t / Math.max(r.m, 0.001);
              const ok = r.m > 0 && ratio >= 1;
              return (
                <div key={r.l}>
                  <div className="flex items-baseline justify-between font-mono text-[11px]">
                    <span className="text-zinc-300">{r.l}</span>
                    <span className={ok ? "text-kx-green" : "text-white"}>
                      {r.m ? r.f(r.m) : "—"} <span className="text-zinc-600">/ {r.f(r.t)} {r.u}</span>
                    </span>
                  </div>
                  <div className="mt-1.5 h-1.5 w-full bg-kx-line">
                    <div
                      className="h-full transition-[width] duration-700"
                      style={{
                        width: `${Math.min(100, (r.m ? ratio : 0) * 100)}%`,
                        backgroundColor: ok ? "#00FF66" : "#00E5FF",
                      }}
                    />
                  </div>
                </div>
              );
            })}
            <p className="border-t border-kx-line pt-3 font-mono text-[10px] leading-relaxed text-zinc-600">
              Targets come from a dedicated single node with the load generator on separate hardware.
              This pod runs both, on shared cores under a CFS quota, behind an extra proxy hop. p50 and
              p95 land close to target; p99 sits near 70ms because CPU throttling parks the whole
              container for most of a 100ms scheduling period once the runner saturates its quota.
            </p>
          </div>
        </Panel>
      </div>

      <Panel
        testid="bench-log-panel"
        title="runner output"
        sub="k6-style summary written by the Go runner"
        right={<Gauge size={14} className="text-zinc-600" strokeWidth={1.5} />}
        pad="p-0"
      >
        <Terminal lines={snap?.log || []} />
      </Panel>
    </div>
  );
}
