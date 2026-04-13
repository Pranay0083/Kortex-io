import { useCallback, useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import {
  Check,
  Copy,
  ExternalLink,
  Loader2,
  Power,
  QrCode,
  Scissors,
  Trash2,
  X,
} from "lucide-react";
import { api, apiError, compact, fmt, shortUrl, prettyUrl, API, ago } from "@/lib/api";
import { Panel, Button, Chip, Empty } from "@/components/kx";

const EXPIRY = [
  { v: "", l: "never" },
  { v: "1h", l: "1 hour" },
  { v: "24h", l: "24 hours" },
  { v: "7d", l: "7 days" },
  { v: "30d", l: "30 days" },
];

function AliasProbe({ alias }) {
  const [res, setRes] = useState(null);
  const [checking, setChecking] = useState(false);
  const timer = useRef();

  useEffect(() => {
    clearTimeout(timer.current);
    if (!alias) {
      setRes(null);
      return;
    }
    setChecking(true);
    timer.current = setTimeout(async () => {
      try {
        const { data } = await api.get(`/links/check-alias?alias=${encodeURIComponent(alias)}`);
        setRes(data);
      } catch {
        setRes(null);
      }
      setChecking(false);
    }, 280);
    return () => clearTimeout(timer.current);
  }, [alias]);

  if (!alias) return null;

  return (
    <div className="mt-2 border border-kx-line bg-black p-3 font-mono text-[10.5px]" data-testid="alias-probe">
      {checking && (
        <div className="flex items-center gap-2 text-zinc-500">
          <Loader2 size={11} className="animate-spin" /> probing bloom filter…
        </div>
      )}
      {!checking && res && (
        <div className="space-y-1.5">
          <div className="flex items-center gap-2">
            <span className="text-zinc-600">bloom</span>
            <span className={res.bloom_maybe ? "text-kx-yellow" : "text-kx-green"}>
              {res.bloom_maybe ? "MAYBE PRESENT" : "DEFINITELY NOT PRESENT"}
            </span>
            <span className="text-zinc-700">{res.bloom_ns}ns</span>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-zinc-600">postgres</span>
            {res.db_checked ? (
              <span className="text-zinc-400">probed shard · {res.db_us}µs</span>
            ) : (
              <span className="text-kx-green">round trip skipped</span>
            )}
          </div>
          {res.bloom_bits?.length > 0 && (
            <div className="flex flex-wrap gap-1 pt-0.5">
              {res.bloom_bits.map((b) => (
                <span key={b} className="border border-kx-line px-1 text-[9px] text-zinc-500">
                  bit {b}
                </span>
              ))}
            </div>
          )}
          <div
            className={`flex items-center gap-1.5 pt-1 font-bold ${
              res.available ? "text-kx-green" : "text-kx-red"
            }`}
            data-testid="alias-verdict"
          >
            {res.available ? <Check size={12} /> : <X size={12} />}
            {res.available ? "available" : "unavailable"} — {res.reason || "invalid format"}
          </div>
        </div>
      )}
    </div>
  );
}

function Created({ result, onClose }) {
  const [copied, setCopied] = useState(false);
  if (!result) return null;
  const url = result.short_url;
  const sf = result.snowflake;

  return (
    <Panel
      testid="created-panel"
      title="link created"
      className="border-kx-cyan/40"
      right={
        <button onClick={onClose} data-testid="close-created" className="text-zinc-500 hover:text-white">
          <X size={14} />
        </button>
      }
    >
      <div className="flex flex-col gap-4 lg:flex-row">
        <div className="min-w-0 flex-1 space-y-3">
          <div className="flex items-center gap-2">
            <code className="min-w-0 flex-1 truncate border border-kx-line bg-black px-3 py-2.5 font-mono text-[12px] text-kx-cyan">
              {url}
            </code>
            <Button
              variant="solid"
              data-testid="copy-created-url"
              onClick={() => {
                navigator.clipboard.writeText(url);
                setCopied(true);
                toast.success("Short URL copied");
                setTimeout(() => setCopied(false), 1600);
              }}
            >
              {copied ? <Check size={13} /> : <Copy size={13} />}
            </Button>
            <a href={url} target="_blank" rel="noreferrer" data-testid="open-created-url">
              <Button variant="ghost">
                <ExternalLink size={13} />
              </Button>
            </a>
          </div>
          <div className="grid grid-cols-2 gap-2 font-mono text-[10.5px] sm:grid-cols-4">
            {[
              ["id gen", `${result.timings.id_gen_us}µs`],
              ["pg write", `${result.timings.pg_write_us}µs`],
              ["redis warm", `${result.timings.redis_us}µs`],
              ["shard", `links_s${result.timings.shard}`],
            ].map(([k, v]) => (
              <div key={k} className="border border-kx-line px-2.5 py-2">
                <div className="text-zinc-600">{k}</div>
                <div className="mt-0.5 text-white">{v}</div>
              </div>
            ))}
          </div>
          <div className="border border-kx-line bg-black p-3">
            <div className="kx-label">snowflake {sf.id}</div>
            <div className="mt-2 flex overflow-hidden font-mono text-[9px]">
              <span className="bg-kx-cyan/20 px-1 py-1 text-kx-cyan">{sf.time_bits}</span>
              <span className="bg-kx-green/20 px-1 py-1 text-kx-green">{sf.node_bits}</span>
              <span className="bg-kx-yellow/20 px-1 py-1 text-kx-yellow">{sf.seq_bits}</span>
            </div>
            <div className="mt-2 flex gap-4 font-mono text-[10px] text-zinc-500">
              <span>ts {new Date(sf.timestamp_ms).toISOString().slice(11, 23)}</span>
              <span>node {sf.node}</span>
              <span>seq {sf.sequence}</span>
            </div>
          </div>
        </div>
        <div className="shrink-0">
          <img
            data-testid="created-qr"
            alt="QR code"
            className="h-[176px] w-[176px] border border-kx-line bg-white p-2"
            src={`${API}/qr/${result.link.code}?url=${encodeURIComponent(prettyUrl(result.link.code))}`}
          />
        </div>
      </div>
    </Panel>
  );
}

export default function Links() {
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(true);
  const [url, setUrl] = useState("");
  const [alias, setAlias] = useState("");
  const [expires, setExpires] = useState("");
  const [permanent, setPermanent] = useState(false);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState(null);
  const [copiedCode, setCopiedCode] = useState("");

  const load = useCallback(async () => {
    try {
      const { data } = await api.get("/links");
      setItems(data.items || []);
    } catch (e) {
      toast.error(apiError(e));
    }
    setLoading(false);
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const submit = async (e) => {
    e.preventDefault();
    setBusy(true);
    try {
      const { data } = await api.post("/links", {
        url,
        alias: alias.trim(),
        expires_in: expires,
        permanent,
      });
      setResult(data);
      setUrl("");
      setAlias("");
      toast.success(`/${data.link.code} → shard ${data.timings.shard}`);
      load();
    } catch (ex) {
      toast.error(apiError(ex, "Could not shorten"));
    }
    setBusy(false);
  };

  const remove = async (code) => {
    try {
      await api.delete(`/links/${code}`);
      toast.success(`${code} deleted`);
      load();
    } catch (e) {
      toast.error(apiError(e));
    }
  };

  const toggle = async (code) => {
    try {
      const { data } = await api.post(`/links/${code}/toggle`);
      toast.success(`${code} ${data.active ? "enabled" : "disabled"}`);
      load();
    } catch (e) {
      toast.error(apiError(e));
    }
  };

  const copy = (code) => {
    navigator.clipboard.writeText(shortUrl(code));
    setCopiedCode(code);
    toast.success("Short URL copied");
    setTimeout(() => setCopiedCode(""), 1600);
  };

  return (
    <div className="space-y-4" data-testid="links-page">
      <div>
        <div className="kx-label">Console / Links</div>
        <h1 className="mt-1.5 font-display text-3xl font-black tracking-tighter lg:text-4xl">
          Write path
        </h1>
      </div>

      <Panel testid="shorten-panel" title="POST /api/links" sub="snowflake → base62 → shard → warm cache">
        <form onSubmit={submit} className="space-y-4" data-testid="shorten-form">
          <div className="grid gap-4 lg:grid-cols-[2fr_1fr]">
            <div>
              <span className="kx-label">Destination URL</span>
              <input
                data-testid="shorten-url-input"
                required
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="https://example.com/a/very/long/path?utm_source=…"
                className="kx-input mt-2 w-full px-3 py-2.5"
              />
            </div>
            <div>
              <span className="kx-label">Custom alias (optional)</span>
              <input
                data-testid="shorten-alias-input"
                value={alias}
                onChange={(e) => setAlias(e.target.value)}
                placeholder="my-launch-post"
                className="kx-input mt-2 w-full px-3 py-2.5"
              />
              <AliasProbe alias={alias.trim()} />
            </div>
          </div>

          <div className="flex flex-wrap items-end gap-4">
            <div>
              <span className="kx-label">Expiry</span>
              <div className="mt-2 flex border border-kx-line">
                {EXPIRY.map((x) => (
                  <button
                    key={x.v || "never"}
                    type="button"
                    data-testid={`expiry-${x.v || "never"}`}
                    onClick={() => setExpires(x.v)}
                    className={`kx-btn px-3 py-2 ${
                      expires === x.v ? "bg-kx-cyan text-black" : "text-zinc-500 hover:text-white"
                    }`}
                  >
                    {x.l}
                  </button>
                ))}
              </div>
            </div>
            <button
              type="button"
              data-testid="permanent-toggle"
              onClick={() => setPermanent(!permanent)}
              className={`kx-btn border px-3 py-2 ${
                permanent ? "border-kx-cyan text-kx-cyan" : "border-kx-line text-zinc-500"
              }`}
            >
              {permanent ? "301 permanent" : "302 temporary"}
            </button>
            <Button type="submit" disabled={busy} className="ml-auto px-6 py-2.5" data-testid="shorten-submit">
              {busy ? <Loader2 size={13} className="animate-spin" /> : <Scissors size={13} />}
              {busy ? "writing…" : "Shorten"}
            </Button>
          </div>
        </form>
      </Panel>

      <Created result={result} onClose={() => setResult(null)} />

      <Panel
        testid="links-table-panel"
        title={`links · ${items.length}`}
        sub="scatter-gather across 4 shards, merged by created_at"
        pad="p-0"
      >
        {loading ? (
          <div className="p-5">
            <Empty>loading shards…</Empty>
          </div>
        ) : items.length === 0 ? (
          <div className="p-5">
            <Empty>no links yet — create one above</Empty>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[860px]">
              <thead>
                <tr className="border-b border-kx-line">
                  {["code", "destination", "shard", "clicks", "24h", "last", "expiry", ""].map((h) => (
                    <th key={h} className="kx-label px-4 py-2.5 text-left">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {items.map(({ link, clicks_24h, clicks_total, last_seen }) => (
                  <tr
                    key={link.code}
                    className="border-b border-kx-line/50 transition-colors hover:bg-kx-hover"
                    data-testid={`link-row-${link.code}`}
                  >
                    <td className="px-4 py-3">
                      <Link
                        to={`/app/links/${link.code}`}
                        className="font-mono text-[12px] font-bold text-kx-cyan hover:underline"
                        data-testid={`link-detail-${link.code}`}
                      >
                        /{link.code}
                      </Link>
                      {link.custom && (
                        <span className="ml-2 font-mono text-[9px] uppercase text-kx-violet">custom</span>
                      )}
                      {!link.active && (
                        <span className="ml-2 font-mono text-[9px] uppercase text-kx-red">off</span>
                      )}
                    </td>
                    <td className="max-w-[320px] px-4 py-3">
                      <div className="truncate text-[12px] text-zinc-300">{link.title}</div>
                      <div className="truncate font-mono text-[10px] text-zinc-600">{link.long_url}</div>
                    </td>
                    <td className="px-4 py-3 font-mono text-[11px] text-zinc-500">links_s{link.shard}</td>
                    <td className="kx-num px-4 py-3 text-[12px] font-bold text-white">
                      {fmt.format(clicks_total || 0)}
                    </td>
                    <td className="kx-num px-4 py-3 text-[12px] text-kx-green">
                      {compact(clicks_24h || 0)}
                    </td>
                    <td className="px-4 py-3 font-mono text-[10px] text-zinc-500">{ago(last_seen)}</td>
                    <td className="px-4 py-3 font-mono text-[10px] text-zinc-500">
                      {link.expires_at ? new Date(link.expires_at).toISOString().slice(0, 10) : "never"}
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex justify-end gap-1">
                        <button
                          data-testid={`copy-${link.code}`}
                          onClick={() => copy(link.code)}
                          className="border border-kx-line p-1.5 text-zinc-500 transition-colors hover:border-kx-cyan hover:text-kx-cyan"
                          title="Copy short URL"
                        >
                          {copiedCode === link.code ? <Check size={12} /> : <Copy size={12} />}
                        </button>
                        <a
                          href={`${API}/qr/${link.code}?url=${encodeURIComponent(prettyUrl(link.code))}`}
                          target="_blank"
                          rel="noreferrer"
                          data-testid={`qr-${link.code}`}
                          className="border border-kx-line p-1.5 text-zinc-500 transition-colors hover:border-kx-cyan hover:text-kx-cyan"
                          title="QR code"
                        >
                          <QrCode size={12} />
                        </a>
                        <button
                          data-testid={`toggle-${link.code}`}
                          onClick={() => toggle(link.code)}
                          className="border border-kx-line p-1.5 text-zinc-500 transition-colors hover:border-kx-yellow hover:text-kx-yellow"
                          title="Enable / disable"
                        >
                          <Power size={12} />
                        </button>
                        <button
                          data-testid={`delete-${link.code}`}
                          onClick={() => remove(link.code)}
                          className="border border-kx-line p-1.5 text-zinc-500 transition-colors hover:border-kx-red hover:text-kx-red"
                          title="Delete"
                        >
                          <Trash2 size={12} />
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Panel>

      <div className="flex flex-wrap gap-2">
        <Chip>rate limit 40 links / min / tenant</Chip>
        <Chip>bloom filter guards every custom alias</Chip>
        <Chip>codes are 10 chars, ~10^17 keyspace</Chip>
      </div>
    </div>
  );
}
