import axios from "axios";

const BASE = process.env.REACT_APP_BACKEND_URL;
export const API = `${BASE}/api`;
export const ORIGIN = BASE;

export const api = axios.create({ baseURL: API });

api.interceptors.request.use((cfg) => {
  const t = localStorage.getItem("kortex_token");
  if (t) cfg.headers.Authorization = `Bearer ${t}`;
  return cfg;
});

export function apiError(e, fallback = "Something went wrong") {
  const d = e?.response?.data?.detail;
  if (typeof d === "string") return d;
  if (Array.isArray(d)) return d.map((x) => x?.msg || JSON.stringify(x)).join(" ");
  return e?.message || fallback;
}

export const shortUrl = (code) => `${API}/r/${code}`;
export const prettyUrl = (code) => `${BASE}/${code}`;

export const fmt = new Intl.NumberFormat("en-US");

export function compact(n) {
  if (n === null || n === undefined) return "0";
  if (n < 1000) return String(Math.round(n));
  if (n < 1e6) return (n / 1000).toFixed(n < 1e4 ? 1 : 0) + "k";
  if (n < 1e9) return (n / 1e6).toFixed(2) + "M";
  return (n / 1e9).toFixed(2) + "B";
}

export function ago(ms) {
  if (!ms) return "—";
  const s = Math.max(0, (Date.now() - ms) / 1000);
  if (s < 60) return `${Math.round(s)}s ago`;
  if (s < 3600) return `${Math.round(s / 60)}m ago`;
  if (s < 86400) return `${Math.round(s / 3600)}h ago`;
  return `${Math.round(s / 86400)}d ago`;
}

export const CHART_COLORS = ["#00E5FF", "#00FF66", "#FFD60A", "#FF3B30", "#8A2BE2", "#7DD3FC"];
