// Shared console primitives. Colours come from theme tokens only (text-kx-*,
// bg-kx-*, border-kx-*), so everything here works in light and dark.

const TONE_TEXT = {
  default: "text-kx-ink",
  accent: "text-kx-accent",
  positive: "text-kx-positive",
  negative: "text-kx-negative",
  warning: "text-kx-warning",
  muted: "text-kx-muted",
};

export const Panel = ({ title, sub, right, children, className = "", testid, pad = "p-5" }) => (
  <section data-testid={testid} className={`kx-panel relative flex flex-col ${className}`}>
    {(title || right) && (
      <header className="flex items-start justify-between gap-4 border-b border-kx-line px-5 py-3.5">
        <div className="min-w-0">
          {title && <h2 className="text-sm font-semibold text-kx-ink">{title}</h2>}
          {sub && <div className="mt-0.5 text-xs text-kx-muted">{sub}</div>}
        </div>
        {right}
      </header>
    )}
    <div className={`flex-1 ${pad}`}>{children}</div>
  </section>
);

/**
 * One headline figure. Use `tone` only when the value is a status
 * (negative for an error count, positive for "healthy"); figures are ink by default.
 */
export const Stat = ({ label, value, unit, sub, tone = "default", testid }) => (
  <div data-testid={testid} className="kx-panel px-4 py-4">
    <div className="kx-label">{label}</div>
    <div className="mt-1.5 flex items-baseline gap-1">
      <span className={`kx-num text-2xl font-semibold ${TONE_TEXT[tone] ?? TONE_TEXT.default}`}>
        {value}
      </span>
      {unit && <span className="text-sm text-kx-muted">{unit}</span>}
    </div>
    {sub && <div className="kx-num mt-1 text-xs text-kx-muted">{sub}</div>}
  </div>
);

/** Small status pill: a pale tint with text of the same hue. */
export const Chip = ({ children, tone = "neutral", className = "" }) => {
  const tones = {
    neutral: "bg-kx-hover text-kx-muted",
    accent: "bg-kx-accent/10 text-kx-accent",
    positive: "bg-kx-positive/10 text-kx-positive",
    negative: "bg-kx-negative/10 text-kx-negative",
    warning: "bg-kx-warning/10 text-kx-warning",
  };
  return (
    <span
      className={`inline-flex flex-none items-center gap-1.5 whitespace-nowrap rounded-full px-2 py-0.5 text-xs font-medium ${tones[tone] ?? tones.neutral} ${className}`}
    >
      {children}
    </span>
  );
};

export const Button = ({ variant = "primary", size = "md", className = "", ...props }) => {
  const styles = {
    primary: "bg-kx-accent text-kx-on-accent hover:bg-kx-accent/90",
    secondary: "border border-kx-line2 bg-kx-surface text-kx-ink hover:bg-kx-hover",
    ghost: "text-kx-muted hover:bg-kx-hover hover:text-kx-ink",
    danger: "border border-kx-negative/40 text-kx-negative hover:bg-kx-negative/10",
  };
  const sizes = { sm: "h-8 px-3", md: "h-9 px-4", lg: "h-11 px-5 text-sm" };
  return (
    <button
      type="button"
      {...props}
      className={`kx-btn inline-flex items-center justify-center gap-2 whitespace-nowrap disabled:cursor-not-allowed disabled:opacity-50 ${styles[variant] ?? styles.primary} ${sizes[size] ?? sizes.md} ${className}`}
    />
  );
};

/** Thin proportional bar; the fill is the data hue unless told otherwise. */
export const Bar = ({ pct, className = "bg-kx-accent" }) => (
  <div className="h-1.5 w-full overflow-hidden rounded-full bg-kx-hover">
    <div
      className={`h-full rounded-full transition-[width] duration-500 ${className}`}
      style={{ width: `${Math.max(0, Math.min(100, pct))}%` }}
    />
  </div>
);

export const ChartTip = ({ active, payload, label, labelFormat }) => {
  if (!active || !payload?.length) return null;
  return (
    <div className="rounded-control border border-kx-line bg-kx-surface px-3 py-2 text-xs shadow-lg">
      <div className="mb-1 text-kx-muted">{labelFormat ? labelFormat(label) : label}</div>
      {payload.map((p, i) => (
        <div key={i} className="flex items-center gap-2 text-kx-ink">
          <span className="h-2 w-2 rounded-full" style={{ background: p.color || p.fill }} />
          {p.name}: <span className="kx-num font-semibold">{Number(p.value).toLocaleString()}</span>
        </div>
      ))}
    </div>
  );
};

/** Empty state: keeps the block's frame and says one plain sentence. */
export const Empty = ({ children, className = "" }) => (
  <div
    className={`flex min-h-[72px] items-center justify-center rounded-control border border-dashed border-kx-line px-4 py-6 text-center text-sm text-kx-muted ${className}`}
  >
    {children}
  </div>
);

/** Loading placeholder: a flat block at the final size, no shimmer. */
export const Skeleton = ({ className = "" }) => (
  <div aria-hidden="true" className={`animate-kx-pulse rounded-control bg-kx-hover ${className}`} />
);

/** Inline error strip for failed polls/loads; keeps stale data visible underneath. */
export const ErrorNote = ({ children, onRetry, testid }) => (
  <div
    role="alert"
    data-testid={testid}
    className="flex items-center justify-between gap-3 rounded-control border border-kx-negative/30 bg-kx-negative/5 px-3 py-2 text-sm text-kx-negative"
  >
    <span>{children}</span>
    {onRetry && (
      <button
        type="button"
        onClick={onRetry}
        className="kx-btn rounded-control px-2 py-0.5 font-medium underline-offset-2 hover:underline"
      >
        Retry
      </button>
    )}
  </div>
);

/** Toggle-button group (range / dim / expiry selectors). */
export const Segmented = ({
  options,
  value,
  onChange,
  label,
  testidPrefix,
  size = "md",
  className = "",
  itemClass = "",
}) => (
  <div
    role="group"
    aria-label={label}
    className={`inline-flex rounded-control border border-kx-line bg-kx-hover p-0.5 ${className}`}
  >
    {options.map((o) => {
      const opt = typeof o === "object" ? o : { v: o, l: o };
      const id = opt.id ?? (opt.v || "auto");
      const active = value === opt.v;
      return (
        <button
          key={id}
          type="button"
          aria-pressed={active}
          data-testid={testidPrefix ? `${testidPrefix}${id}` : undefined}
          onClick={() => onChange(opt.v)}
          className={`kx-btn rounded-[5px] ${size === "sm" ? "px-2.5 py-1 text-xs" : "px-3 py-1.5"} ${
            active ? "bg-kx-surface text-kx-ink shadow-sm" : "text-kx-muted hover:text-kx-ink"
          } ${itemClass}`}
        >
          {opt.l || "auto"}
        </button>
      );
    })}
  </div>
);

/** Small square icon-only button; `label` is required for screen readers. */
export const IconButton = ({ label, tone = "default", className = "", children, ...props }) => {
  const tones = {
    default: "hover:bg-kx-hover hover:text-kx-ink",
    negative: "hover:bg-kx-negative/10 hover:text-kx-negative",
  };
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      {...props}
      className={`inline-flex h-8 w-8 items-center justify-center rounded-control text-kx-muted transition-colors disabled:opacity-40 ${tones[tone] ?? tones.default} ${className}`}
    >
      {children}
    </button>
  );
};

/** Page title row used by every console screen. */
export const PageHeader = ({ title, sub, right, testid }) => (
  <div className="mb-6 flex flex-wrap items-end justify-between gap-4">
    <div className="min-w-0">
      <h1 data-testid={testid} className="text-2xl font-semibold text-kx-ink">
        {title}
      </h1>
      {sub && <p className="mt-1 text-sm text-kx-muted">{sub}</p>}
    </div>
    {right && <div className="flex items-center gap-2">{right}</div>}
  </div>
);
