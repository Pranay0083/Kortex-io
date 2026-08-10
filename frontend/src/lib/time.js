const pad = (n) => String(n).padStart(2, "0");
const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

const hhmm = (d) => `${pad(d.getHours())}:${pad(d.getMinutes())}`;
const mmmdd = (d) => `${MONTHS[d.getMonth()]} ${pad(d.getDate())}`;

/**
 * Build an x-axis formatter for a time series bucketed at `gran` ("minute" | "hour" | "day")
 * covering `spanMs`. Uses the browser's local timezone (backend labels are UTC strings).
 * Hourly/minute ranges longer than a day include the date.
 */
export function timeFormatter(gran, spanMs = 0) {
  const multiDay = spanMs > 26 * 3600 * 1000;
  return (t) => {
    if (t === null || t === undefined || t === "") return "";
    const d = new Date(Number(t));
    if (Number.isNaN(d.getTime())) return "";
    if (gran === "day") return mmmdd(d);
    return multiDay ? `${mmmdd(d)} ${hhmm(d)}` : hhmm(d);
  };
}

/** Infer granularity from bucket spacing when the server doesn't say. */
export function inferGran(points) {
  if (!points || points.length < 2) return "hour";
  const step = Number(points[1].t) - Number(points[0].t);
  if (step <= 60_000) return "minute";
  if (step <= 3_600_000) return "hour";
  return "day";
}

/**
 * Formatter for a timeseries API payload: { points, granularity, from, to }.
 * `full` (tooltips) always includes the date for sub-day buckets.
 */
export function seriesFormatter(payload, { full = false } = {}) {
  const pts = payload?.points || [];
  const gran = payload?.granularity || inferGran(pts);
  const span =
    payload?.from && payload?.to
      ? payload.to - payload.from
      : pts.length > 1
        ? Number(pts[pts.length - 1].t) - Number(pts[0].t)
        : 0;
  return timeFormatter(gran, full ? Infinity : span);
}

/** Local wall-clock time with milliseconds, e.g. 14:03:22.481 */
export function clockMs(ms) {
  const d = new Date(ms);
  return `${hhmm(d)}:${pad(d.getSeconds())}.${String(d.getMilliseconds()).padStart(3, "0")}`;
}

/** Local date + time, e.g. 2026-10-31 14:03:22 */
export function localDateTime(ms) {
  const d = new Date(ms);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${hhmm(d)}:${pad(d.getSeconds())}`;
}

export function localDate(ms) {
  const d = new Date(ms);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}
