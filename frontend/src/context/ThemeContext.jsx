import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";

const STORAGE_KEY = "kx-theme";
const ThemeContext = createContext(null);

const readStored = () => {
  try {
    const t = localStorage.getItem(STORAGE_KEY);
    return t === "light" || t === "dark" ? t : null;
  } catch {
    return null;
  }
};

const systemTheme = () =>
  typeof window !== "undefined" && window.matchMedia?.("(prefers-color-scheme: dark)").matches
    ? "dark"
    : "light";

/**
 * Light / dark theme. An explicit choice is stored; without one the app
 * follows the OS setting and keeps following it if it changes.
 */
export function ThemeProvider({ children }) {
  const [stored, setStored] = useState(readStored);
  const [system, setSystem] = useState(systemTheme);
  const theme = stored ?? system;

  useEffect(() => {
    const mq = window.matchMedia?.("(prefers-color-scheme: dark)");
    if (!mq) return undefined;
    const onChange = (e) => setSystem(e.matches ? "dark" : "light");
    mq.addEventListener?.("change", onChange);
    return () => mq.removeEventListener?.("change", onChange);
  }, []);

  useEffect(() => {
    document.documentElement.setAttribute("data-theme", theme);
  }, [theme]);

  const setTheme = useCallback((t) => {
    setStored(t);
    try {
      localStorage.setItem(STORAGE_KEY, t);
    } catch {
      /* private mode: the choice lasts for this tab only */
    }
  }, []);

  const toggle = useCallback(
    () => setTheme(theme === "dark" ? "light" : "dark"),
    [theme, setTheme],
  );

  const value = useMemo(() => ({ theme, setTheme, toggle }), [theme, setTheme, toggle]);
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme() {
  const ctx = useContext(ThemeContext);
  // Outside a provider (isolated component tests) behave as a fixed light theme.
  return ctx ?? { theme: "light", setTheme: () => {}, toggle: () => {} };
}

const TOKENS = [
  "ink",
  "ink2",
  "muted",
  "faint",
  "line",
  "line2",
  "surface",
  "bg",
  "hover",
  "accent",
  "positive",
  "negative",
  "warning",
];

/**
 * Resolved colours for SVG / recharts props, which need real colour strings.
 * Re-computed whenever the theme flips.
 *
 *   accent   the one data hue: the main series, the current selection
 *   ink      secondary series drawn in the text colour
 *   muted    tertiary series, axis text
 *   grid     chart gridlines
 *   track    unfilled bar tracks
 *   positive / negative / warning   status only
 */
export function useChartColors() {
  const { theme } = useTheme();
  return useMemo(() => {
    const css = typeof document !== "undefined" ? getComputedStyle(document.documentElement) : null;
    const out = {};
    for (const t of TOKENS) {
      const raw = css?.getPropertyValue(`--kx-${t}`).trim();
      out[t] = raw ? `rgb(${raw.split(/\s+/).join(", ")})` : "currentColor";
    }
    out.grid = out.line;
    out.track = out.hover;
    out.theme = theme;
    return out;
  }, [theme]);
}
