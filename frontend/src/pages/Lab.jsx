import { useCallback, useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { ArrowLeft, Binary, Boxes, CircuitBoard, Play, RotateCcw, Search, Zap } from "lucide-react";
import { api, apiError, CHART_COLORS, compact, fmt } from "@/lib/api";
import { Panel, Stat, Chip, Button, Empty, Bar as MiniBar } from "@/components/kx";

/* ---------------- Snowflake decoder ---------------- */

function BitStrip({ parts }) {
  if (!parts) return null;
  const seg = [
    { bits: parts.time_bits, c: "#00E5FF", l: "41-bit timestamp (ms since 2025-01-01)" },
    { bits: parts.node_bits, c: "#00FF66", l: "10-bit node id" },
    { bits: parts.seq_bits, c: "#FFD60A", l: "12-bit sequence" },
  ];
  return (
    <div>
      <div className="flex overflow-hidden border border-kx-line">
        {seg.map((s) => (
          <div
            key={s.l}
            className="min-w-0 px-1 py-2 font-mono text-[9px] leading-tight tracking-tight"
            style={{ background: `${s.c}18`, color: s.c, flex: s.bits.length }}
          >
            <div className="break-all">{s.bits}</div>
          </div>
        ))}
      </div>
      <div className="mt-2 flex flex-wrap gap-x-5 gap-y-1 font-mono text-[10px]">
        {seg.map((s) => (
          <span key={s.l} className="flex items-center gap-1.5" style={{ color: s.c }}>
            <span className="h-2 w-2" style={{ background: s.c }} />
            {s.l}
          </span>
        ))}
      </div>
    </div>
  );
}

function SnowflakeLab() {
  const [input, setInput] = useState("");
  const [parts, setParts] = useState(null);
  const [burst, setBurst] = useState(null);
  const [busy, setBusy] = useState(false);
  const [custom, setCustom] = useState(false);

  const decode = useCallback(async (value) => {
    const q = /^\d{10,}$/.test(value) ? `id=${value}` : `code=${encodeURIComponent(value)}`;
    try {
      const { data } = await api.get(`/lab/snowflake?${q}`);
      setParts(data.parts);
      setCustom(value !== "" && data.parts.code !== value);
    } catch (e) {
      toast.error(apiError(e, "Could not decode"));
    }
  }, []);

  useEffect(() => {
    api.get("/lab/snowflake").then(({ data }) => {
      setParts(data.parts);
      setInput(data.parts.code);
    });
  }, []);

  const mint = async () => {
    const { data } = await api.get("/lab/snowflake");
    setParts(data.parts);
    setInput(data.parts.code);
    setCustom(false);
  };

  const runBurst = async () => {
    setBusy(true);
    try {
      const { data } = await api.get("/lab/snowflake/burst?n=200000");
      setBurst(data);
    } catch (e) {
      toast.error(apiError(e));
    }
    setBusy(false);
  };

  return (
    <Panel
      testid="lab-snowflake"
      title="01 · snowflake + base62"
      sub="why not an auto-increment, and why not a hash"
    >
      <div className="grid gap-6 lg:grid-cols-[1.1fr_1fr]">
        <div>
          <div className="flex gap-2">
            <input
              data-testid="snowflake-input"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && decode(input)}
              placeholder="paste a short code or a raw id"
              className="kx-input min-w-0 flex-1 px-3 py-2.5"
            />
            <Button variant="primary" onClick={() => decode(input)} data-testid="snowflake-decode-btn">
              <Search size={12} /> Decode
            </Button>
            <Button variant="ghost" onClick={mint} data-testid="snowflake-mint-btn">
              Mint
            </Button>
          </div>

          <div className="mt-5" data-testid="snowflake-bits">
            <BitStrip parts={parts} />
          </div>

          {custom && (
            <div className="mt-3 border border-kx-yellow/40 bg-kx-yellow/5 px-3 py-2 font-mono text-[10.5px] text-kx-yellow">
              that input is a custom alias, not a generated code — the bits below are noise. Custom
              aliases are stored, not encoded.
            </div>
          )}

          {parts && (
            <div className="mt-5 grid grid-cols-2 gap-2 font-mono text-[10.5px] sm:grid-cols-3">
              {[
                ["code", parts.code],
                ["raw id", parts.id],
                ["scrambled", parts.scrambled],
                ["minted at", new Date(parts.timestamp_ms).toISOString().replace("T", " ").slice(0, 23)],
                ["node", parts.node],
                ["sequence", parts.sequence],
              ].map(([k, v]) => (
                <div key={k} className="border border-kx-line px-2.5 py-2">
                  <div className="text-zinc-600">{k}</div>
                  <div className="mt-0.5 truncate text-white">{String(v)}</div>
                </div>
              ))}
            </div>
          )}

          <p className="mt-5 text-[13px] leading-relaxed text-zinc-400">
            The code is not stored to be decoded — it decodes itself. The 64-bit ID is multiplied by a
            fixed odd constant modulo 2<sup>58</sup>, which is a bijection, so consecutive IDs land far
            apart in code space and nobody can enumerate your links by incrementing. Multiplying by the
            modular inverse gets the ID back with no database round trip.
          </p>
        </div>

        <div>
          <div className="kx-panel bg-black p-4">
            <div className="kx-label">the three options</div>
            <table className="mt-3 w-full font-mono text-[10.5px]">
              <thead>
                <tr className="border-b border-kx-line">
                  <th className="py-2 text-left text-zinc-600">approach</th>
                  <th className="py-2 text-left text-zinc-600">coordination</th>
                  <th className="py-2 text-left text-zinc-600">collision</th>
                  <th className="py-2 text-left text-zinc-600">guessable</th>
                </tr>
              </thead>
              <tbody>
                <tr className="border-b border-kx-line/50">
                  <td className="py-2 text-zinc-300">DB auto-increment</td>
                  <td className="py-2 text-kx-red">every write</td>
                  <td className="py-2 text-kx-green">none</td>
                  <td className="py-2 text-kx-red">trivially</td>
                </tr>
                <tr className="border-b border-kx-line/50">
                  <td className="py-2 text-zinc-300">hash(url) prefix</td>
                  <td className="py-2 text-kx-green">none</td>
                  <td className="py-2 text-kx-red">birthday bound</td>
                  <td className="py-2 text-kx-yellow">same url = same code</td>
                </tr>
                <tr>
                  <td className="py-2 text-white">snowflake + base62</td>
                  <td className="py-2 text-kx-green">node id only</td>
                  <td className="py-2 text-kx-green">impossible by construction</td>
                  <td className="py-2 text-kx-green">scrambled</td>
                </tr>
              </tbody>
            </table>
          </div>

          <div className="mt-3 flex items-center justify-between">
            <div className="kx-label">generator burst test</div>
            <Button variant="solid" onClick={runBurst} disabled={busy} data-testid="snowflake-burst-btn">
              <Play size={11} /> {busy ? "running…" : "Mint 200k IDs"}
            </Button>
          </div>

          {burst ? (
            <div className="mt-3 grid grid-cols-2 gap-2" data-testid="burst-result">
              <Stat label="ids / second" value={compact(burst.ids_per_sec)} accent="#00E5FF" />
              <Stat label="collisions" value={burst.collisions} accent={burst.collisions ? "#FF3B30" : "#00FF66"} />
              <Stat label="unique" value={fmt.format(burst.unique)} />
              <Stat
                label="sequence exhausted"
                value={burst.seq_exhausted}
                sub="waited for next ms"
                accent="#FFD60A"
              />
            </div>
          ) : (
            <div className="mt-3">
              <Empty>4096 ids per node per millisecond — prove it</Empty>
            </div>
          )}
        </div>
      </div>
    </Panel>
  );
}

/* ---------------- Collision math ---------------- */

function CollisionLab() {
  const [len, setLen] = useState(7);
  const [links, setLinks] = useState(1e9);

  const space = useMemo(() => Math.pow(62, len), [len]);
  const prob = useMemo(() => {
    // birthday bound: 1 - e^(-n^2 / 2N)
    const x = (links * links) / (2 * space);
    return 1 - Math.exp(-x);
  }, [links, space]);

  return (
    <Panel
      testid="lab-collision"
      title="02 · why random codes need luck"
      sub="birthday bound over a base62 keyspace"
    >
      <div className="grid gap-6 lg:grid-cols-2">
        <div className="space-y-5">
          <div>
            <div className="flex justify-between">
              <span className="kx-label">code length</span>
              <span className="kx-num text-sm text-kx-cyan">{len} chars</span>
            </div>
            <input
              type="range"
              min="4"
              max="12"
              value={len}
              data-testid="collision-length"
              onChange={(e) => setLen(Number(e.target.value))}
              className="mt-2 w-full accent-kx-cyan"
            />
          </div>
          <div>
            <div className="flex justify-between">
              <span className="kx-label">links generated</span>
              <span className="kx-num text-sm text-kx-cyan">{compact(links)}</span>
            </div>
            <input
              type="range"
              min="6"
              max="12"
              step="0.1"
              value={Math.log10(links)}
              data-testid="collision-links"
              onChange={(e) => setLinks(Math.pow(10, Number(e.target.value)))}
              className="mt-2 w-full accent-kx-cyan"
            />
          </div>
          <div className="grid grid-cols-2 gap-2">
            <Stat label="keyspace" value={space.toExponential(2)} />
            <Stat
              label="collision probability"
              value={prob > 0.999 ? ">99.9%" : `${(prob * 100).toFixed(prob < 0.01 ? 4 : 2)}%`}
              accent={prob > 0.05 ? "#FF3B30" : "#00FF66"}
              testid="collision-prob"
            />
          </div>
        </div>
        <div className="text-[13px] leading-relaxed text-zinc-400">
          <p>
            With random codes you are always gambling. At 7 characters and a billion links the birthday
            bound puts a collision somewhere near certainty, which means every insert needs a uniqueness
            check and a retry loop — an extra read on the hot write path, and a tail latency cliff once
            the table gets dense.
          </p>
          <p className="mt-4">
            Snowflake sidesteps the whole question. Uniqueness is structural: no two IDs from the same
            node share a millisecond-and-sequence pair, and no two nodes share a node ID. The retry loop
            disappears, the uniqueness index becomes a safety net instead of a dependency, and codes stay
            sortable by creation time, which is exactly the clustering a time-series-shaped workload
            wants.
          </p>
          <p className="mt-4 font-mono text-[11px] text-zinc-600">
            Kortex emits 10-character codes — 62<sup>10</sup> ≈ 8.4×10<sup>17</sup> — with zero collision
            probability by construction. Custom aliases are the only path that can collide, which is what
            the bloom filter below is for.
          </p>
        </div>
      </div>
    </Panel>
  );
}

/* ---------------- Consistent hashing ---------------- */

function RingLab() {
  const [from, setFrom] = useState(4);
  const [to, setTo] = useState(5);
  const [vnodes, setVnodes] = useState(160);
  const [rep, setRep] = useState(null);
  const [busy, setBusy] = useState(false);

  const run = useCallback(async () => {
    setBusy(true);
    try {
      const { data } = await api.get(`/lab/ring?from=${from}&to=${to}&vnodes=${vnodes}&samples=20000`);
      setRep(data);
    } catch (e) {
      toast.error(apiError(e));
    }
    setBusy(false);
  }, [from, to, vnodes]);

  useEffect(() => {
    run();
  }, [run]);

  const R = 118;
  const cx = 140;
  const cy = 140;

  return (
    <Panel
      testid="lab-ring"
      title="03 · consistent hashing"
      sub="what a resize actually costs you"
      right={
        <Button variant="ghost" onClick={run} disabled={busy} data-testid="ring-run-btn">
          <RotateCcw size={11} /> resample
        </Button>
      }
    >
      <div className="grid gap-6 lg:grid-cols-[280px_1fr]">
        <div>
          <svg width="280" height="280" viewBox="0 0 280 280" data-testid="ring-svg">
            <circle cx={cx} cy={cy} r={R} fill="none" stroke="#1f1f1f" strokeWidth="18" />
            {rep?.nodes.map((n, i) => {
              const a = ((n.angle - 90) * Math.PI) / 180;
              return (
                <circle
                  key={i}
                  cx={cx + Math.cos(a) * R}
                  cy={cy + Math.sin(a) * R}
                  r="5"
                  fill={CHART_COLORS[n.shard % CHART_COLORS.length]}
                />
              );
            })}
            <text
              x={cx}
              y={cy - 6}
              textAnchor="middle"
              fill="#fff"
              fontFamily="JetBrains Mono"
              fontSize="30"
              fontWeight="700"
            >
              {to}
            </text>
            <text
              x={cx}
              y={cy + 16}
              textAnchor="middle"
              fill="#52525b"
              fontFamily="JetBrains Mono"
              fontSize="10"
              letterSpacing="2"
            >
              SHARDS
            </text>
          </svg>
          <div className="mt-2 flex flex-wrap gap-2">
            {Array.from({ length: to }).map((_, i) => (
              <span key={i} className="flex items-center gap-1.5 font-mono text-[10px] text-zinc-500">
                <span className="h-2 w-2" style={{ background: CHART_COLORS[i % CHART_COLORS.length] }} />
                links_s{i}
              </span>
            ))}
          </div>
        </div>

        <div className="space-y-5">
          <div className="grid gap-4 sm:grid-cols-3">
            {[
              ["shards before", from, setFrom, 1, 12],
              ["shards after", to, setTo, 1, 12],
              ["vnodes / shard", vnodes, setVnodes, 1, 400],
            ].map(([label, val, set, min, max]) => (
              <div key={label}>
                <div className="flex justify-between">
                  <span className="kx-label">{label}</span>
                  <span className="kx-num text-xs text-kx-cyan">{val}</span>
                </div>
                <input
                  type="range"
                  min={min}
                  max={max}
                  value={val}
                  data-testid={`ring-${String(label).replace(/[^a-z]/g, "")}`}
                  onChange={(e) => set(Number(e.target.value))}
                  className="mt-2 w-full accent-kx-cyan"
                />
              </div>
            ))}
          </div>

          {rep && (
            <>
              <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
                <Stat
                  testid="ring-moved"
                  label="keys remapped"
                  value={`${rep.moved_pct.toFixed(1)}%`}
                  accent="#00FF66"
                  sub="consistent hashing"
                />
                <Stat
                  testid="ring-naive"
                  label="naive modulo"
                  value={`${rep.naive_moved_pct.toFixed(1)}%`}
                  accent="#FF3B30"
                  sub="hash(key) % N"
                />
                <Stat
                  label="ideal minimum"
                  value={`${(from < to ? (1 - from / to) * 100 : (1 - to / from) * 100).toFixed(1)}%`}
                  sub="theoretical floor"
                />
              </div>

              <div>
                <div className="kx-label mb-2">key distribution after resize</div>
                <div className="space-y-2">
                  {Object.entries(rep.after)
                    .sort((a, b) => Number(a[0]) - Number(b[0]))
                    .map(([shard, count]) => (
                      <div key={shard}>
                        <div className="flex justify-between font-mono text-[10.5px]">
                          <span className="text-zinc-400">links_s{shard}</span>
                          <span className="text-zinc-500">
                            {fmt.format(count)} · {((count / rep.samples) * 100).toFixed(1)}%
                          </span>
                        </div>
                        <div className="mt-1">
                          <MiniBar
                            pct={(count / rep.samples) * 100 * (to > 2 ? to : 2)}
                            color={CHART_COLORS[Number(shard) % CHART_COLORS.length]}
                          />
                        </div>
                      </div>
                    ))}
                </div>
              </div>

              <p className="text-[13px] leading-relaxed text-zinc-400">
                Going from {from} to {to} shards with modulo would rehash{" "}
                <span className="text-kx-red">{rep.naive_moved_pct.toFixed(1)}%</span> of all codes — a
                full table rewrite and a cold cache. The ring moves{" "}
                <span className="text-kx-green">{rep.moved_pct.toFixed(1)}%</span>, essentially just the
                slice the new shard claims. More vnodes buys you a flatter distribution at the cost of a
                larger sorted ring to binary-search on every lookup.
              </p>
            </>
          )}
        </div>
      </div>
    </Panel>
  );
}

/* ---------------- Bloom filter ---------------- */

function BloomLab() {
  const [state, setState] = useState(null);
  const [key, setKey] = useState("");
  const [probe, setProbe] = useState(null);
  const [fp, setFp] = useState(0.01);
  const [sweep, setSweep] = useState(null);
  const [busy, setBusy] = useState(false);

  const load = async () => {
    const { data } = await api.get("/lab/bloom");
    setState(data);
  };

  useEffect(() => {
    load();
  }, []);

  const add = async () => {
    if (!key.trim()) return;
    const { data } = await api.post("/lab/bloom/add", { key: key.trim() });
    setState(data);
    setProbe(null);
    setKey("");
    toast.success(`inserted "${data.added}" → bits ${data.bits.join(", ")}`);
  };

  const test = async () => {
    if (!key.trim()) return;
    const { data } = await api.get(`/lab/bloom/test?key=${encodeURIComponent(key.trim())}`);
    setProbe(data);
  };

  const reset = async () => {
    const { data } = await api.post("/lab/bloom/reset", { expected: 1000, fp_rate: fp });
    setState(data);
    setProbe(null);
    toast.message("filter reset");
  };

  const runSweep = async () => {
    setBusy(true);
    try {
      const { data } = await api.get(`/lab/bloom/sweep?inserts=5000&probes=50000&fp_rate=${fp}`);
      setSweep(data);
    } catch (e) {
      toast.error(apiError(e));
    }
    setBusy(false);
  };

  const st = state?.stats;

  return (
    <Panel
      testid="lab-bloom"
      title="04 · bloom filter in front of the shard"
      sub="a definite no is worth a whole network round trip"
    >
      <div className="grid gap-6 lg:grid-cols-[1fr_1fr]">
        <div>
          <div className="flex gap-2">
            <input
              data-testid="bloom-key-input"
              value={key}
              onChange={(e) => setKey(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && add()}
              placeholder="alias to insert or probe"
              className="kx-input min-w-0 flex-1 px-3 py-2.5"
            />
            <Button variant="primary" onClick={add} data-testid="bloom-add-btn">
              Insert
            </Button>
            <Button variant="ghost" onClick={test} data-testid="bloom-test-btn">
              Probe
            </Button>
            <Button variant="ghost" onClick={reset} data-testid="bloom-reset-btn">
              <RotateCcw size={12} />
            </Button>
          </div>

          {probe && (
            <div
              data-testid="bloom-verdict"
              className={`mt-3 border px-3 py-2.5 font-mono text-[11px] ${
                !probe.maybe
                  ? "border-kx-green/40 bg-kx-green/5 text-kx-green"
                  : probe.actually_present
                    ? "border-kx-cyan/40 bg-kx-cyan/5 text-kx-cyan"
                    : "border-kx-red/40 bg-kx-red/5 text-kx-red"
              }`}
            >
              {probe.verdict}
              <div className="mt-1 text-[10px] opacity-70">
                probed bits {probe.bits.join(" · ")}
              </div>
            </div>
          )}

          <div className="mt-4">
            <div className="kx-label mb-2">bit array · {st ? compact(st.m) : "—"} bits, downsampled</div>
            <div
              className="grid gap-[2px]"
              style={{ gridTemplateColumns: "repeat(32, minmax(0, 1fr))" }}
              data-testid="bloom-bitmap"
            >
              {(state?.preview || []).map((c, i) => (
                <div
                  key={i}
                  className="aspect-square"
                  style={{
                    background: c === 0 ? "#151515" : `rgba(0,229,255,${Math.min(1, 0.25 + c * 0.18)})`,
                  }}
                />
              ))}
            </div>
          </div>

          {st && (
            <div className="mt-4 grid grid-cols-2 gap-2 sm:grid-cols-4">
              <Stat label="inserted (n)" value={st.n} />
              <Stat label="fill ratio" value={`${(st.fill_ratio * 100).toFixed(1)}%`} accent="#00E5FF" />
              <Stat label="est. fpr" value={`${(st.est_fpr * 100).toFixed(2)}%`} accent="#FFD60A" />
              <Stat label="memory" value={`${st.memory_kb.toFixed(1)} KB`} />
            </div>
          )}
        </div>

        <div>
          <p className="text-[13px] leading-relaxed text-zinc-400">
            Custom aliases are the only place Kortex can genuinely collide, and checking them means
            asking the owning Postgres shard. Most of those checks are wasted: the alias is free. A bloom
            filter answers <span className="text-kx-green">definitely not present</span> in nanoseconds
            from a few hundred KB of RAM, and only the maybes pay for the round trip. It never says no
            when the answer is yes, so the shard remains the source of truth — the filter just removes
            the boring traffic.
          </p>

          <div className="mt-5">
            <div className="flex justify-between">
              <span className="kx-label">target false-positive rate</span>
              <span className="kx-num text-xs text-kx-cyan">{(fp * 100).toFixed(2)}%</span>
            </div>
            <input
              type="range"
              min="0.001"
              max="0.2"
              step="0.001"
              value={fp}
              data-testid="bloom-fp-slider"
              onChange={(e) => setFp(Number(e.target.value))}
              className="mt-2 w-full accent-kx-yellow"
            />
            <Button
              variant="solid"
              onClick={runSweep}
              disabled={busy}
              className="mt-3 w-full py-2.5"
              data-testid="bloom-sweep-btn"
            >
              <Zap size={12} /> {busy ? "sweeping…" : "Measure real FPR · 5k inserts / 50k probes"}
            </Button>
          </div>

          {sweep && (
            <div className="mt-4 space-y-2" data-testid="bloom-sweep-result">
              <div className="grid grid-cols-2 gap-2">
                <Stat
                  label="measured fpr"
                  value={`${(sweep.measured_fpr * 100).toFixed(3)}%`}
                  accent="#00FF66"
                />
                <Stat label="predicted fpr" value={`${(sweep.predicted_fpr * 100).toFixed(3)}%`} />
                <Stat label="lookup" value={`${sweep.lookup_ns}`} unit="ns" accent="#00E5FF" />
                <Stat label="memory" value={`${sweep.memory_kb.toFixed(0)}`} unit="KB" />
              </div>
              <div className="font-mono text-[10px] text-zinc-600">
                m = {fmt.format(sweep.m)} bits · k = {sweep.k} hashes · {sweep.false_positives} false
                positives out of {fmt.format(sweep.probes)} probes
              </div>
            </div>
          )}
        </div>
      </div>
    </Panel>
  );
}

/* ---------------- page ---------------- */

export default function Lab() {
  return (
    <div className="min-h-screen bg-kx-bg">
      <header className="sticky top-0 z-50 border-b border-kx-line bg-black/85 backdrop-blur-xl">
        <div className="mx-auto flex max-w-[1400px] items-center justify-between px-5 py-3.5 lg:px-10">
          <Link to="/" className="flex items-center gap-2.5" data-testid="lab-home-link">
            <ArrowLeft size={14} className="text-zinc-500" />
            <div className="h-3 w-3 bg-kx-cyan" />
            <span className="font-display text-lg font-black tracking-tighter">KORTEX</span>
          </Link>
          <div className="flex items-center gap-2">
            <Chip tone="cyan">design lab</Chip>
            <Link
              to="/app"
              data-testid="lab-console-link"
              className="kx-btn border border-kx-line px-4 py-2 text-zinc-300 hover:border-zinc-500 hover:text-white"
            >
              Console
            </Link>
          </div>
        </div>
      </header>

      <div className="mx-auto max-w-[1400px] px-5 py-12 lg:px-10 lg:py-16">
        <div className="max-w-3xl">
          <div className="kx-label">Interactive write-ups</div>
          <h1 className="mt-3 font-display text-4xl font-black leading-[0.95] tracking-tighter lg:text-6xl">
            The parts of the design that are actually arguable.
          </h1>
          <p className="mt-6 text-base leading-relaxed text-zinc-400">
            Every widget below hits the live Go service. The bit decoder, the ring resampler, the bloom
            sweep — all of them execute real code paths on the same process serving redirects, so the
            numbers move when the system is under load.
          </p>
          <div className="mt-6 flex flex-wrap gap-2">
            <Chip>
              <Binary size={10} /> id generation
            </Chip>
            <Chip>
              <CircuitBoard size={10} /> sharding
            </Chip>
            <Chip>
              <Boxes size={10} /> probabilistic filters
            </Chip>
          </div>
        </div>

        <div className="mt-12 space-y-4">
          <SnowflakeLab />
          <CollisionLab />
          <RingLab />
          <BloomLab />
        </div>

        <div className="mt-10 kx-panel p-6">
          <div className="kx-label">05 · materialized views vs on-read aggregation</div>
          <p className="mt-3 max-w-3xl text-[13px] leading-relaxed text-zinc-400">
            The last comparison needs your own data, so it lives in the console. Open any link and press{" "}
            <span className="text-white">Race</span> — it runs the identical question twice, once against
            the pre-aggregated rollups and once as a full scan over the raw columns, and reports both
            wall times and the number of values touched.
          </p>
          <Link
            to="/app"
            data-testid="lab-to-race"
            className="kx-btn mt-5 inline-flex bg-kx-cyan px-6 py-3 text-black hover:bg-white"
          >
            Open the console
          </Link>
        </div>
      </div>
    </div>
  );
}
