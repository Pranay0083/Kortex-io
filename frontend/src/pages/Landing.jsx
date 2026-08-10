import { useCallback, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { ArrowRight } from "lucide-react";
import { api, apiError, fmt } from "@/lib/api";
import { usePolling } from "@/hooks/usePolling";
import { primeSystemInfo, systemInfoFrom } from "@/hooks/useSystemInfo";
import { useAuth } from "@/context/AuthContext";
import { Skeleton } from "@/components/kx";
import Logo from "@/components/Logo";
import ThemeToggle from "@/components/ThemeToggle";

// Link-styled buttons. Same look as <Button>, but they navigate.
const BTN = "kx-btn inline-flex items-center justify-center gap-2 whitespace-nowrap";
const PRIMARY = `${BTN} bg-kx-accent text-kx-on-accent hover:bg-kx-accent/90`;
const SECONDARY = `${BTN} border border-kx-line2 bg-kx-surface text-kx-ink hover:bg-kx-hover`;
const WRAP = "mx-auto max-w-[1200px] px-4 sm:px-6 lg:px-10";

// Only what the product does today; each line maps to a shipped feature.
const FEATURES = [
  { t: "Custom aliases", d: "Pick your own short code; availability is checked as you type." },
  { t: "Expiry", d: "Links can expire after an hour, a day, a week or a month." },
  { t: "301 or 302", d: "Choose a permanent or a temporary redirect per link." },
  { t: "QR codes", d: "Every link has a QR code you can download as a PNG." },
  { t: "On and off", d: "Disable a link without deleting it, and turn it back on later." },
  {
    t: "Click analytics",
    d: "Clicks over time, by referrer, device, browser and OS, with bots flagged.",
  },
  { t: "Live clicks", d: "Watch clicks arrive a couple of seconds after they happen." },
];

function Nav({ authed }) {
  return (
    <header className="sticky top-0 z-50 border-b border-kx-line bg-kx-bg/90 backdrop-blur">
      <div className={`${WRAP} flex h-14 items-center justify-between gap-3`}>
        <Logo testid="landing-logo" />
        <nav className="flex items-center gap-1 sm:gap-1.5">
          {!authed && (
            <Link
              to="/login"
              data-testid="landing-login-btn"
              className={`${BTN} hidden h-8 px-3 text-kx-ink2 hover:bg-kx-hover hover:text-kx-ink sm:inline-flex`}
            >
              Sign in
            </Link>
          )}
          <Link to="/app" data-testid="landing-cta-btn" className={`${PRIMARY} h-8 px-3.5`}>
            Open console
          </Link>
          <ThemeToggle className="ml-1" />
        </nav>
      </div>
    </header>
  );
}

/** "4m ago" from the server's uptime in seconds. */
function uptimeAgo(sec) {
  if (sec < 60) return "just now";
  if (sec < 3600) return `${Math.round(sec / 60)}m ago`;
  if (sec < 86400) return `${Math.round(sec / 3600)}h ago`;
  return `${Math.round(sec / 86400)}d ago`;
}

function LiveStrip({ stats, failed }) {
  const since = stats?.uptime_s != null ? `since restart, ${uptimeAgo(stats.uptime_s)}` : null;
  const links = stats?.shards?.reduce((n, s) => n + (s.links || 0), 0);
  const items = [
    { l: "Short links", v: stats?.shards ? fmt.format(links) : null },
    { l: "Clicks recorded", v: stats ? fmt.format(stats.olap.total_rows) : null },
    {
      l: "Redirects served",
      v: stats ? (stats.redirects > 0 ? fmt.format(stats.redirects) : "—") : null,
      sub: since,
    },
  ];
  return (
    <div className="border-y border-kx-line bg-kx-surface" data-testid="landing-live-strip">
      <div className={`${WRAP} grid grid-cols-1 sm:grid-cols-3`}>
        {items.map((it, i) => (
          <div
            key={it.l}
            className={`py-4 ${i > 0 ? "border-t border-kx-line sm:border-l sm:border-t-0 sm:pl-6" : "sm:pr-6"}`}
          >
            <div className="kx-label">{it.l}</div>
            {it.v === null && !failed ? (
              <Skeleton className="mt-1.5 h-7 w-20" />
            ) : (
              <div
                className={`kx-num mt-1 text-xl font-semibold ${it.v && it.v !== "—" ? "text-kx-ink" : "text-kx-muted"}`}
              >
                {it.v ?? "—"}
              </div>
            )}
            <div className="mt-0.5 h-4 text-xs text-kx-faint">{it.sub ?? ""}</div>
          </div>
        ))}
      </div>
    </div>
  );
}

export default function Landing() {
  const { user, demoLogin } = useAuth() ?? {};
  const navigate = useNavigate();
  const [stats, setStats] = useState(null);
  const [statsFailed, setStatsFailed] = useState(false);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      const { data } = await api.get("/system/stats");
      setStats(data);
      primeSystemInfo(data);
      setStatsFailed(false);
    } catch {
      setStatsFailed(true);
    }
  }, []);

  usePolling(load, 4000);

  const { nodeName } = systemInfoFrom(stats);
  const canDemo = !user && stats?.demo_mode === true;

  const tryDemo = async () => {
    setBusy(true);
    try {
      await demoLogin();
      navigate("/app");
    } catch (e) {
      toast.error(apiError(e, "Could not start the demo"));
      setBusy(false);
    }
  };

  return (
    <div className="min-h-screen bg-kx-bg text-kx-ink">
      <Nav authed={Boolean(user)} />

      <section
        className={`${WRAP} grid grid-cols-1 items-start gap-10 py-12 lg:grid-cols-[1.05fr_1fr] lg:gap-16 lg:py-20`}
      >
        <div className="min-w-0 lg:pt-6">
          <h1 className="max-w-[15ch] text-[40px] font-semibold leading-[1.08] text-kx-ink sm:text-5xl lg:text-[56px]">
            Short links, with every click counted.
          </h1>
          <p className="mt-5 max-w-xl text-base leading-relaxed text-kx-ink2 lg:text-[17px]">
            Kortex shortens your links and records every click as it happens: where it came from,
            what opened it, and whether it was a bot.
          </p>
          <div className="mt-7 flex flex-wrap items-center gap-3">
            <Link
              to={user ? "/app/links" : "/register"}
              data-testid="hero-primary-cta"
              className={`${PRIMARY} h-11 px-5 text-sm`}
            >
              {user ? "Shorten a link" : "Create an account"}
              <ArrowRight size={15} strokeWidth={2} aria-hidden="true" />
            </Link>
            {canDemo ? (
              <button
                type="button"
                onClick={tryDemo}
                disabled={busy}
                data-testid="hero-demo-btn"
                className={`${SECONDARY} h-11 px-5 text-sm disabled:opacity-50`}
              >
                {busy ? "Opening the demo…" : "Try the demo"}
              </button>
            ) : (
              !user && (
                <Link
                  to="/login"
                  data-testid="hero-secondary-cta"
                  className={`${SECONDARY} h-11 px-5 text-sm`}
                >
                  Sign in
                </Link>
              )
            )}
          </div>
        </div>

        <div className="kx-panel" data-testid="landing-features">
          <h2 className="border-b border-kx-line px-5 py-3.5 text-sm font-semibold text-kx-ink">
            Every link comes with
          </h2>
          <ul>
            {FEATURES.map((f, i) => (
              <li key={f.t} className={`px-5 py-3.5 ${i > 0 ? "border-t border-kx-line" : ""}`}>
                <div className="text-sm font-medium text-kx-ink">{f.t}</div>
                <div className="mt-0.5 text-sm leading-relaxed text-kx-muted">{f.d}</div>
              </li>
            ))}
          </ul>
        </div>
      </section>

      <LiveStrip stats={stats} failed={statsFailed} />

      <footer>
        <div
          className={`${WRAP} flex flex-col justify-between gap-6 py-10 sm:flex-row sm:items-end`}
        >
          <div className="max-w-sm">
            <Logo />
            <p className="mt-3 text-sm leading-relaxed text-kx-muted">
              A URL shortener with real-time click analytics, built in Go with Postgres and Redis.
            </p>
            <p className="kx-num mt-3 text-xs text-kx-faint">
              {nodeName ? `v1.0 · ${nodeName}` : "v1.0"}
            </p>
          </div>
          <div className="flex gap-6 text-sm">
            <Link to="/app" className="text-kx-ink2 hover:text-kx-accent">
              Console
            </Link>
            {!user && (
              <Link to="/login" className="text-kx-ink2 hover:text-kx-accent">
                Sign in
              </Link>
            )}
          </div>
        </div>
      </footer>
    </div>
  );
}
