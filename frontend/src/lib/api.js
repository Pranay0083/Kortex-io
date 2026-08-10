import axios from "axios";

/** Absolute origin of the Kortex server. Empty / unset → same origin (Docker, `yarn dev` proxy). */
export const ORIGIN = String(
  import.meta.env.REACT_APP_BACKEND_URL || window.location.origin,
).replace(/\/+$/, "");
export const API = `${ORIGIN}/api`;

/** Browser auth is cookie-only: the server sets httpOnly access/refresh cookies. */
export const api = axios.create({ baseURL: API, withCredentials: true });

/* ---- 401 → refresh → retry -------------------------------------------- */

// Requests that must never trigger a refresh: they *are* the auth flow.
const NO_REFRESH = ["/auth/login", "/auth/register", "/auth/refresh", "/auth/logout"];

let refreshing = null;
let onSessionExpired = null;

/** Called once a refresh attempt fails; AuthContext uses it to clear state and redirect. */
export function setSessionExpiredHandler(fn) {
  onSessionExpired = fn;
}

/** Single-flight: concurrent 401s share one POST /auth/refresh. Resolves to the refreshed user. */
export function refreshSession() {
  if (!refreshing) {
    refreshing = api
      .post("/auth/refresh", null, { skipAuthRefresh: true })
      .then(({ data }) => data?.user ?? null)
      .finally(() => {
        refreshing = null;
      });
  }
  return refreshing;
}

api.interceptors.response.use(
  (res) => res,
  async (error) => {
    const cfg = error?.config;
    const url = cfg?.url || "";
    if (
      error?.response?.status !== 401 ||
      !cfg ||
      cfg._retried ||
      cfg.skipAuthRefresh ||
      NO_REFRESH.some((p) => url.startsWith(p))
    ) {
      throw error;
    }
    try {
      await refreshSession();
    } catch {
      onSessionExpired?.();
      throw error;
    }
    return api({ ...cfg, _retried: true });
  },
);

/* ---- helpers ------------------------------------------------------------- */

export function apiError(e, fallback = "Something went wrong") {
  const d = e?.response?.data?.detail;
  if (typeof d === "string") return d;
  if (Array.isArray(d)) return d.map((x) => x?.msg || JSON.stringify(x)).join(" ");
  return e?.message || fallback;
}

/** Redirect endpoint — always works, even without SPA routing. */
export const shortUrl = (code) => `${API}/r/${code}`;
/** Pretty short link served directly by the Go server at /{code}. */
export const prettyUrl = (code) => `${ORIGIN}/${code}`;
/** QR PNG encoding the pretty short link. */
export const qrUrl = (code) => `${API}/qr/${code}?url=${encodeURIComponent(prettyUrl(code))}`;

export const fmt = new Intl.NumberFormat("en-US");

export function compact(n) {
  if (n === null || n === undefined || Number.isNaN(n)) return "0";
  if (n < 1000) return String(Math.round(n));
  if (n < 1e6) return (n / 1000).toFixed(n < 1e4 ? 1 : 0) + "k";
  if (n < 1e9) return (n / 1e6).toFixed(2) + "M";
  return (n / 1e9).toFixed(2) + "B";
}

export function ago(ms, now = Date.now()) {
  if (!ms) return "—";
  const s = Math.max(0, (now - ms) / 1000);
  if (s < 60) return `${Math.round(s)}s ago`;
  if (s < 3600) return `${Math.round(s / 60)}m ago`;
  if (s < 86400) return `${Math.round(s / 3600)}h ago`;
  return `${Math.round(s / 86400)}d ago`;
}

export const CHART_COLORS = ["#00E5FF", "#00FF66", "#FFD60A", "#FF3B30", "#8A2BE2", "#7DD3FC"];
