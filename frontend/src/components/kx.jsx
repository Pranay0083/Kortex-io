export const Panel = ({ title, sub, right, children, className = "", testid, pad = "p-5" }) => (
  <section data-testid={testid} className={`kx-panel relative flex flex-col ${className}`}>
    {(title || right) && (
      <header className="flex items-start justify-between gap-4 border-b border-kx-line px-5 py-3">
        <div>
          {title && <div className="kx-label">{title}</div>}
          {sub && <div className="mt-1 text-xs text-zinc-500">{sub}</div>}
        </div>
        {right}
      </header>
    )}
    <div className={`flex-1 ${pad}`}>{children}</div>
  </section>
);

export const Stat = ({ label, value, unit, sub, accent = "#FFFFFF", testid }) => (
  <div
    data-testid={testid}
    className="kx-panel group border-l-2 border-l-kx-line px-4 py-4 transition-colors hover:border-l-kx-cyan"
  >
    <div className="kx-label">{label}</div>
    <div className="mt-2 flex items-baseline gap-1.5">
      <span className="kx-num text-2xl font-bold lg:text-3xl" style={{ color: accent }}>
        {value}
      </span>
      {unit && <span className="kx-num text-xs text-zinc-500">{unit}</span>}
    </div>
    {sub && <div className="mt-1.5 font-mono text-[11px] text-zinc-600">{sub}</div>}
  </div>
);

export const Chip = ({ children, tone = "zinc" }) => {
  const tones = {
    zinc: "border-kx-line text-zinc-400",
    cyan: "border-kx-cyan/40 text-kx-cyan bg-kx-cyan/5",
    green: "border-kx-green/40 text-kx-green bg-kx-green/5",
    red: "border-kx-red/40 text-kx-red bg-kx-red/5",
    yellow: "border-kx-yellow/40 text-kx-yellow bg-kx-yellow/5",
  };
  return (
    <span
      className={`inline-flex items-center gap-1.5 border px-2 py-0.5 font-mono text-[10px] font-bold uppercase tracking-widest ${tones[tone]}`}
    >
      {children}
    </span>
  );
};

export const Button = ({ variant = "primary", className = "", ...props }) => {
  const styles = {
    primary: "bg-kx-cyan text-black hover:bg-white",
    ghost: "border border-kx-line text-white hover:border-zinc-500 hover:bg-kx-hover",
    danger: "border border-kx-red/40 text-kx-red hover:bg-kx-red/10",
    solid: "bg-white text-black hover:bg-zinc-200",
  };
  return (
    <button
      {...props}
      className={`kx-btn inline-flex items-center justify-center gap-2 px-4 py-2 disabled:cursor-not-allowed disabled:opacity-40 ${styles[variant]} ${className}`}
    />
  );
};

export const Bar = ({ pct, color = "#00E5FF" }) => (
  <div className="h-1 w-full bg-kx-line">
    <div
      className="h-full transition-[width] duration-500"
      style={{ width: `${Math.min(100, pct)}%`, backgroundColor: color }}
    />
  </div>
);

export const ChartTip = ({ active, payload, label }) => {
  if (!active || !payload?.length) return null;
  return (
    <div className="border border-kx-line2 bg-black px-3 py-2 font-mono text-[11px]">
      <div className="mb-1 text-zinc-500">{label}</div>
      {payload.map((p, i) => (
        <div key={i} className="flex items-center gap-2 text-white">
          <span className="h-2 w-2" style={{ background: p.color || p.fill }} />
          {p.name}: <span className="font-bold">{Number(p.value).toLocaleString()}</span>
        </div>
      ))}
    </div>
  );
};

export const Empty = ({ children }) => (
  <div className="flex h-full min-h-[140px] items-center justify-center border border-dashed border-kx-line font-mono text-xs text-zinc-600">
    {children}
  </div>
);
