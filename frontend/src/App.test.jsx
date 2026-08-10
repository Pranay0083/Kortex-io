import { afterEach, describe, expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import App from "@/App";
import { countCalls, mockApi } from "@/test/mockApi";

const NOW = Date.now();
const USER = { id: "2", email: "dev@kortex.dev", role: "user" };

const stats = {
  node_name: "kortex-test-node",
  shard_count: 4,
  node: "legacy-name",
  uptime_s: 125,
  redirects: 1234,
  cache: {
    hits: 90,
    misses: 10,
    hit_rate: 90,
    keys: 50,
    warmed: 40,
    evicted_keys: 0,
    used_memory_mb: 1.5,
    eviction_policy: "allkeys-lru",
  },
  bus: {
    topic: "clicks",
    produced: 100,
    consumed: 100,
    total_lag: 0,
    dropped: 0,
    partitions: [{ id: 0, high_watermark: 10, committed: 10, lag: 0, retained: 10 }],
  },
  olap: {
    total_rows: 100,
    part_count: 2,
    merges: 1,
    partitions: [{ day: "2026-10-31", parts: 2, rows: 100 }],
    mv_rows: 20,
    dict_entries: 30,
    approx_mem_mb: 2,
  },
  bloom: { m: 1000, k: 7, memory_kb: 1, est_fpr: 0.01, db_skipped: 3 },
  shards: [0, 1, 2, 3].map((i) => ({ shard: i, table: `links_s${i}`, links: i, clicks: i * 10 })),
  pg_pool: { total_conns: 4, idle_conns: 3, acquired_conns: 1, max_conns: 10, acquire_count: 9 },
  id_gen: { issued: 5, node_id: 1, seq_exhausted: 0, clock_backwards: 0 },
  runtime: { goroutines: 12, heap_mb: 3, gc_cycles: 4, cpus: 2, go_version: "go1.24" },
};

const dim = (k) => [{ key: k, count: 10, pct: 100 }];
const routes = (authed) => ({
  "GET /auth/me": authed ? [200, USER] : [401, {}],
  "POST /auth/refresh": [401, {}],
  "GET /system/stats": [200, stats],
  "GET /bench/status": [200, { running: false, series: [], log: [] }],
  "GET /analytics/overview": [
    200,
    {
      summary: {
        total: 10,
        last_24h: 5,
        last_1h: 1,
        bots: 1,
        first_seen: NOW - 1e6,
        last_seen: NOW,
      },
      links: 3,
      geo: dim("IN"),
      referrer: dim("direct"),
      device: dim("desktop"),
      browser: dim("Chrome"),
      query_us: 120,
    },
  ],
  "GET /analytics/all/timeseries": [
    200,
    {
      points: [
        { t: NOW - 3_600_000, label: "x", count: 3 },
        { t: NOW, label: "y", count: 4 },
      ],
      granularity: "hour",
      from: NOW - 86_400_000,
      to: NOW,
      query_us: 80,
    },
  ],
  "GET /stream/recent": [
    200,
    {
      events: [
        {
          ts: NOW,
          code: "abc",
          country: "IN",
          city: "Pune",
          referrer: "x",
          device: "d",
          status: 302,
        },
      ],
      bus: stats.bus,
    },
  ],
});

function renderAt(path) {
  window.history.pushState({}, "", path);
  return render(<App />);
}

describe("App routes", () => {
  let calls;
  afterEach(() => calls?.restore());

  it("renders the landing page with the reported node name", async () => {
    calls = mockApi(routes(false));
    renderAt("/");
    expect(await screen.findByText(/v1\.0 · kortex-test-node/)).toBeInTheDocument();
  });

  it("offers the shared demo account only in demo mode", async () => {
    const DEMO = { id: "9", email: "demo@kortex.dev", role: "demo" };
    calls = mockApi({
      ...routes(false),
      "GET /system/stats": [200, { ...stats, demo_mode: true }],
      "POST /auth/demo": [200, { user: DEMO }],
    });
    renderAt("/login");
    expect(await screen.findByTestId("login-email")).toHaveValue("");
    fireEvent.click(await screen.findByTestId("login-demo-btn"));
    expect(await screen.findByTestId("shell-demo-note")).toHaveTextContent("Shared demo account");
    expect(countCalls(calls, "POST /auth/demo")).toBe(1);
    expect(countCalls(calls, "POST /auth/login")).toBe(0);
  });

  it("hides the demo button when demo mode is off", async () => {
    calls = mockApi(routes(false));
    renderAt("/login");
    await screen.findByTestId("login-form");
    await new Promise((r) => setTimeout(r, 0));
    expect(screen.queryByTestId("login-demo-btn")).not.toBeInTheDocument();
  });

  it("renders a 404 for unknown multi-segment routes", async () => {
    calls = mockApi(routes(false));
    renderAt("/no/such/page");
    expect(await screen.findByTestId("not-found-page")).toHaveTextContent("/no/such/page");
  });

  it("renders the dashboard for a signed-in user", async () => {
    calls = mockApi(routes(true));
    renderAt("/app");
    expect(await screen.findByText("Traffic across 3 links")).toBeInTheDocument();
    expect(await screen.findByText("/abc")).toBeInTheDocument();
    expect(screen.getByText("Top referrers")).toBeInTheDocument();
    expect(screen.queryByText("Top countries")).not.toBeInTheDocument();
    expect(screen.getByTestId("range-24h")).toHaveAttribute("aria-pressed", "true");
    const ov = calls.find((c) => c.key === "GET /analytics/overview");
    expect(ov.url).toContain("range=24h");
  });

  it("shows an embedded 404 inside the console", async () => {
    calls = mockApi(routes(true));
    renderAt("/app/nope");
    expect(await screen.findByTestId("not-found-page")).toBeInTheDocument();
    expect(screen.getByTestId("logout-btn")).toBeInTheDocument();
  });

  it("keeps the benchmark away from customers", async () => {
    calls = mockApi(routes(true));
    renderAt("/app/bench");
    expect(await screen.findByText("Traffic across 3 links")).toBeInTheDocument();
    expect(screen.queryByTestId("nav-bench")).not.toBeInTheDocument();
  });

  it("shows the benchmark to the admin", async () => {
    calls = mockApi({ ...routes(true), "GET /auth/me": [200, { ...USER, role: "admin" }] });
    renderAt("/app/bench");
    expect(await screen.findByTestId("bench-page")).toBeInTheDocument();
    expect(screen.getByTestId("nav-bench")).toBeInTheDocument();
    expect(screen.getByTestId("run-benchmark-btn")).toBeEnabled();
  });

  it("keeps the last ingest result visible after a load test", async () => {
    calls = mockApi({
      ...routes(true),
      "GET /auth/me": [200, { ...USER, role: "admin" }],
      "GET /bench/status": [
        200,
        {
          kind: "redirect",
          running: false,
          phase: "done",
          rps: 50000,
          requests: 500000,
          series: [],
          log: [],
          last_ingest: { kind: "ingest", ingest_rate: 1200000, events_stored: 500000 },
        },
      ],
    });
    renderAt("/app/bench");
    expect(await screen.findByTestId("bench-rps")).toHaveTextContent("50,000");
    expect(screen.getByText("500k rows stored")).toBeInTheDocument();
  });

  it("renders infra stats using node_name", async () => {
    calls = mockApi(routes(true));
    renderAt("/app/ops");
    expect(
      await screen.findByRole("heading", { level: 1, name: "Infrastructure" }),
    ).toBeInTheDocument();
    // The heading renders while loading; the node name arrives with the stats
    // (it also appears in the sidebar, hence findAll).
    expect((await screen.findAllByText("kortex-test-node")).length).toBeGreaterThan(0);
    expect(screen.getByText("Postgres shards · 4")).toBeInTheDocument();
  });

  it("shows an inline error when the dashboard can't load", async () => {
    calls = mockApi({ ...routes(true), "GET /analytics/overview": [500, { detail: "olap down" }] });
    renderAt("/app");
    expect(await screen.findByTestId("dashboard-error")).toHaveTextContent("olap down");
  });
});
