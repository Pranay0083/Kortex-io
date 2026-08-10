import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import {
  Check,
  ChevronLeft,
  ChevronRight,
  Copy,
  ExternalLink,
  Loader2,
  MoreHorizontal,
  Power,
  QrCode,
  Search,
  Trash2,
  X,
} from "lucide-react";
import { api, apiError, compact, fmt, prettyUrl, qrUrl, ago } from "@/lib/api";
import { copyText } from "@/lib/clipboard";
import { usePolling } from "@/hooks/usePolling";
import { localDate } from "@/lib/time";
import {
  Panel,
  Button,
  Chip,
  Empty,
  Segmented,
  IconButton,
  Skeleton,
  PageHeader,
} from "@/components/kx";
import ConfirmDialog from "@/components/ConfirmDialog";

const PAGE_SIZE = 25;

const EXPIRY = [
  { v: "", l: "Never", id: "never" },
  { v: "1h", l: "1 hour" },
  { v: "24h", l: "24 hours" },
  { v: "7d", l: "7 days" },
  { v: "30d", l: "30 days" },
];

const plural = (n, one, many = `${one}s`) => `${fmt.format(n)} ${n === 1 ? one : many}`;

function AliasProbe({ alias }) {
  // { alias, data } for the most recent probe; anything else means "still checking".
  const [probe, setProbe] = useState(null);

  useEffect(() => {
    if (!alias) return undefined;
    let alive = true;
    const t = setTimeout(async () => {
      let data = null;
      try {
        ({ data } = await api.get(`/links/check-alias?alias=${encodeURIComponent(alias)}`));
      } catch {
        data = null;
      }
      if (alive) setProbe({ alias, data });
    }, 280);
    return () => {
      alive = false;
      clearTimeout(t);
    };
  }, [alias]);

  if (!alias) return null;
  const checking = probe?.alias !== alias;
  const res = checking ? null : probe.data;

  return (
    <div className="mt-1.5 min-h-[20px] text-xs" data-testid="alias-probe" aria-live="polite">
      {checking && (
        <div className="flex items-center gap-1.5 text-kx-muted">
          <Loader2 size={12} className="animate-spin" aria-hidden="true" /> Checking availability…
        </div>
      )}
      {!checking && res && (
        <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
          <span
            className={`inline-flex items-center gap-1 font-medium ${
              res.available ? "text-kx-positive" : "text-kx-negative"
            }`}
            data-testid="alias-verdict"
          >
            {res.available ? (
              <Check size={13} aria-hidden="true" />
            ) : (
              <X size={13} aria-hidden="true" />
            )}
            {res.available ? "Available" : "Unavailable"} — {res.reason || "invalid format"}
          </span>
          <span className="text-kx-faint">
            {res.db_checked ? `checked in ${res.db_us} µs` : `checked in ${res.bloom_ns} ns`}
          </span>
        </div>
      )}
    </div>
  );
}

function Created({ result, onClose }) {
  const [copied, setCopied] = useState(false);
  if (!result) return null;
  const { code, long_url: longUrl } = result.link;
  // Always absolute and client-derived, so it's right behind any proxy.
  const url = prettyUrl(code);

  return (
    <Panel
      testid="created-panel"
      title="Link created"
      right={
        <IconButton
          label="Dismiss"
          onClick={onClose}
          data-testid="close-created"
          className="-my-1.5 -mr-2"
        >
          <X size={16} aria-hidden="true" />
        </IconButton>
      }
    >
      <div className="flex items-start gap-4">
        <img
          data-testid="created-qr"
          alt={`QR code for ${url}`}
          className="h-24 w-24 shrink-0 rounded-control border border-kx-line"
          src={qrUrl(code)}
        />
        <div className="min-w-0 flex-1">
          <code
            className="block truncate font-mono text-base font-medium text-kx-ink"
            data-testid="created-short-url"
          >
            {url}
          </code>
          {longUrl && <div className="mt-0.5 truncate text-xs text-kx-muted">{longUrl}</div>}
          <div className="mt-3 flex flex-wrap gap-2">
            <Button
              variant="secondary"
              size="sm"
              data-testid="copy-created-url"
              aria-label="Copy short URL"
              onClick={async () => {
                if (await copyText(url, { success: "Short URL copied" })) {
                  setCopied(true);
                  setTimeout(() => setCopied(false), 1600);
                }
              }}
            >
              {copied ? (
                <Check size={14} aria-hidden="true" />
              ) : (
                <Copy size={14} aria-hidden="true" />
              )}
              {copied ? "Copied" : "Copy"}
            </Button>
            <a
              href={url}
              target="_blank"
              rel="noreferrer"
              data-testid="open-created-url"
              aria-label="Open short URL in a new tab"
              className="kx-btn inline-flex h-8 items-center gap-2 whitespace-nowrap border border-kx-line2 bg-kx-surface px-3 text-kx-ink hover:bg-kx-hover"
            >
              <ExternalLink size={14} aria-hidden="true" /> Open
            </a>
            <Link
              to={`/app/links/${code}`}
              className="kx-btn inline-flex h-8 items-center whitespace-nowrap px-3 text-kx-muted hover:bg-kx-hover hover:text-kx-ink"
            >
              View analytics
            </Link>
          </div>
        </div>
      </div>
    </Panel>
  );
}

/** "⋯" row menu: QR, enable/disable, delete. Closes on Esc, outside click and selection. */
function RowMenu({ link, onToggle, onDelete }) {
  const [open, setOpen] = useState(false);
  const wrap = useRef(null);
  const btn = useRef(null);
  const menu = useRef(null);
  const code = link.code;

  useEffect(() => {
    if (!open) return undefined;
    menu.current?.querySelector('[role="menuitem"]')?.focus();
    const onDown = (e) => {
      if (wrap.current && !wrap.current.contains(e.target)) setOpen(false);
    };
    const onKey = (e) => {
      if (e.key === "Escape") {
        setOpen(false);
        btn.current?.focus();
      }
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const onMenuKey = (e) => {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    e.preventDefault();
    const items = [...menu.current.querySelectorAll('[role="menuitem"]')];
    const i = items.indexOf(document.activeElement);
    const next =
      e.key === "ArrowDown" ? (i + 1) % items.length : (i - 1 + items.length) % items.length;
    items[next]?.focus();
  };

  const item =
    "flex w-full items-center gap-2.5 rounded-[5px] px-2.5 py-1.5 text-left text-sm outline-none transition-colors focus-visible:bg-kx-hover";

  return (
    <div ref={wrap} className="relative">
      <IconButton
        ref={btn}
        label={`More actions for /${code}`}
        aria-haspopup="menu"
        aria-expanded={open}
        data-testid={`menu-${code}`}
        onClick={() => setOpen((o) => !o)}
        className={open ? "bg-kx-hover text-kx-ink" : ""}
      >
        <MoreHorizontal size={16} aria-hidden="true" />
      </IconButton>
      {open && (
        <div
          ref={menu}
          role="menu"
          tabIndex={-1}
          aria-label={`Actions for /${code}`}
          onKeyDown={onMenuKey}
          className="absolute right-0 top-full z-20 mt-1 w-44 rounded-card border border-kx-line bg-kx-surface p-1 shadow-lg"
        >
          <a
            role="menuitem"
            href={qrUrl(code)}
            target="_blank"
            rel="noreferrer"
            data-testid={`qr-${code}`}
            onClick={() => setOpen(false)}
            className={`${item} text-kx-ink hover:bg-kx-hover`}
          >
            <QrCode size={15} className="text-kx-muted" aria-hidden="true" /> QR code
          </a>
          <button
            type="button"
            role="menuitem"
            data-testid={`toggle-${code}`}
            onClick={() => {
              setOpen(false);
              onToggle(code);
            }}
            className={`${item} text-kx-ink hover:bg-kx-hover`}
          >
            <Power size={15} className="text-kx-muted" aria-hidden="true" />
            {link.active ? "Disable link" : "Enable link"}
          </button>
          <div className="my-1 border-t border-kx-line" />
          <button
            type="button"
            role="menuitem"
            data-testid={`delete-${code}`}
            onClick={() => {
              setOpen(false);
              onDelete(code);
            }}
            className={`${item} text-kx-negative hover:bg-kx-negative/10`}
          >
            <Trash2 size={15} aria-hidden="true" /> Delete
          </button>
        </div>
      )}
    </div>
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
  const [query, setQuery] = useState("");
  const [page, setPage] = useState(0);
  const [pending, setPending] = useState(null); // code awaiting delete confirmation
  const [deleting, setDeleting] = useState(false);

  const load = useCallback(async () => {
    try {
      const { data } = await api.get("/links");
      setItems(data.items || []);
    } catch (e) {
      toast.error(apiError(e));
    }
    setLoading(false);
  }, []);

  // Keeps click counts fresh while the tab is visible.
  usePolling(load, 15000);

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
      toast.success(`/${data.link.code} is ready`);
      load();
    } catch (ex) {
      toast.error(apiError(ex, "Could not shorten"));
    }
    setBusy(false);
  };

  const remove = async (code) => {
    setDeleting(true);
    try {
      await api.delete(`/links/${code}`);
      toast.success(`/${code} deleted`);
      load();
    } catch (e) {
      // Seeded demo links come back 403 "demo links are protected".
      toast.error(apiError(e, "Could not delete"));
    }
    setDeleting(false);
    setPending(null);
  };

  const toggle = async (code) => {
    try {
      const { data } = await api.post(`/links/${code}/toggle`);
      toast.success(`/${code} ${data.active ? "enabled" : "disabled"}`);
      load();
    } catch (e) {
      toast.error(apiError(e, "Could not update the link"));
    }
  };

  const copy = async (code) => {
    if (await copyText(prettyUrl(code), { success: "Short URL copied" })) {
      setCopiedCode(code);
      setTimeout(() => setCopiedCode(""), 1600);
    }
  };

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return items;
    return items.filter(({ link }) =>
      [link.code, link.title, link.long_url].some((f) => f && String(f).toLowerCase().includes(q)),
    );
  }, [items, query]);

  const totalClicks = useMemo(
    () => items.reduce((sum, it) => sum + (it.clicks_total || 0), 0),
    [items],
  );

  const pages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
  const current = Math.min(page, pages - 1);
  const visible = filtered.slice(current * PAGE_SIZE, (current + 1) * PAGE_SIZE);

  const th = "kx-label px-4 py-2.5 font-medium";

  return (
    <div className="space-y-5" data-testid="links-page">
      <PageHeader
        title="Links"
        sub={
          loading ? "Loading…" : `${plural(items.length, "link")} · ${plural(totalClicks, "click")}`
        }
      />

      <Panel testid="shorten-panel" title="Create a link">
        <form onSubmit={submit} data-testid="shorten-form">
          <div className="grid gap-4 lg:grid-cols-[2fr_1fr]">
            <div>
              <label htmlFor="shorten-url" className="kx-label">
                Destination URL
              </label>
              <input
                id="shorten-url"
                type="url"
                data-testid="shorten-url-input"
                required
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="https://example.com/a/very/long/path"
                className="kx-input mt-1.5 h-9 w-full px-3"
              />
            </div>
            <div>
              <label htmlFor="shorten-alias" className="kx-label">
                Custom alias (optional)
              </label>
              <input
                id="shorten-alias"
                data-testid="shorten-alias-input"
                value={alias}
                onChange={(e) => setAlias(e.target.value)}
                placeholder="my-launch-post"
                className="kx-input mt-1.5 h-9 w-full px-3"
              />
              <AliasProbe alias={alias.trim()} />
            </div>
          </div>

          <div className="mt-4 flex flex-wrap items-end gap-x-6 gap-y-4">
            <div>
              <span className="kx-label block">Expires</span>
              <Segmented
                className="mt-1.5 flex-wrap"
                label="Expires"
                options={EXPIRY}
                value={expires}
                onChange={setExpires}
                testidPrefix="expiry-"
                itemClass="max-sm:px-2"
              />
            </div>
            <label className="flex h-9 cursor-pointer items-center gap-2 text-sm text-kx-ink2">
              <input
                type="checkbox"
                data-testid="permanent-toggle"
                checked={permanent}
                onChange={(e) => setPermanent(e.target.checked)}
                className="h-4 w-4 accent-kx-accent"
              />
              Permanent redirect (301)
            </label>
            <Button
              type="submit"
              disabled={busy}
              className="w-full sm:ml-auto sm:w-auto"
              data-testid="shorten-submit"
            >
              {busy && <Loader2 size={14} className="animate-spin" aria-hidden="true" />}
              {busy ? "Shortening…" : "Shorten"}
            </Button>
          </div>
        </form>
      </Panel>

      <Created result={result} onClose={() => setResult(null)} />

      <Panel
        testid="links-table-panel"
        title="All links"
        sub={
          query ? `${fmt.format(filtered.length)} of ${fmt.format(items.length)} match` : undefined
        }
        pad="p-0"
        right={
          <div className="relative -my-1 w-40 sm:w-60">
            <Search
              size={14}
              aria-hidden="true"
              className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-kx-faint"
            />
            <input
              type="search"
              aria-label="Search links by code, title or URL"
              data-testid="links-search"
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                setPage(0);
              }}
              placeholder="Search links"
              className="kx-input h-8 w-full pl-8 pr-2 text-sm"
            />
          </div>
        }
      >
        {loading ? (
          <div className="space-y-2 p-5" aria-busy="true">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </div>
        ) : items.length === 0 ? (
          <div className="p-5">
            <Empty>No links yet. Create one above.</Empty>
          </div>
        ) : filtered.length === 0 ? (
          <div className="p-5">
            <Empty>No links match “{query}”.</Empty>
          </div>
        ) : (
          <>
            {/* Below md the rows collapse to two lines: link + clicks, then the destination. */}
            <table className="block w-full md:table">
              <thead className="hidden md:table-header-group">
                <tr className="border-b border-kx-line">
                  <th className={`${th} text-left`}>Link</th>
                  <th className={`${th} text-left`}>Destination</th>
                  <th className={`${th} text-right`}>Clicks</th>
                  <th className={`${th} whitespace-nowrap text-right`}>Last 24h</th>
                  <th className={`${th} whitespace-nowrap text-right`}>Last click</th>
                  <th className={`${th} text-right`}>Expires</th>
                  <th className="px-2 py-2.5">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody className="block md:table-row-group">
                {visible.map(({ link, clicks_24h, clicks_total, last_seen }) => (
                  <tr
                    key={link.code}
                    className="grid grid-cols-[minmax(0,1fr)_auto_auto] items-center gap-x-3 border-b border-kx-line px-4 py-2.5 transition-colors last:border-b-0 hover:bg-kx-hover/60 md:table-row md:p-0"
                    data-testid={`link-row-${link.code}`}
                  >
                    <td className="col-start-1 row-start-1 min-w-0 md:whitespace-nowrap md:px-4 md:py-3">
                      <div className="flex min-w-0 items-center gap-2">
                        <Link
                          to={`/app/links/${link.code}`}
                          className={`truncate font-mono text-[13px] font-medium hover:underline ${
                            link.active ? "text-kx-ink" : "text-kx-muted"
                          }`}
                          data-testid={`link-detail-${link.code}`}
                        >
                          /{link.code}
                        </Link>
                        {link.custom && <Chip className="hidden sm:inline-flex">Custom</Chip>}
                        {!link.active && <Chip tone="warning">Disabled</Chip>}
                      </div>
                    </td>
                    <td className="col-span-2 col-start-1 row-start-2 min-w-0 md:w-full md:max-w-0 md:px-4 md:py-3">
                      {link.title && (
                        <div className="hidden truncate text-sm text-kx-ink md:block">
                          {link.title}
                        </div>
                      )}
                      <div className="truncate font-mono text-xs text-kx-muted">
                        {link.long_url}
                      </div>
                    </td>
                    <td className="kx-num col-start-2 row-start-1 text-right text-sm font-semibold text-kx-ink md:px-4 md:py-3">
                      {fmt.format(clicks_total || 0)}
                    </td>
                    <td className="kx-num hidden text-right text-sm text-kx-ink md:table-cell md:px-4 md:py-3">
                      {compact(clicks_24h || 0)}
                    </td>
                    <td className="hidden whitespace-nowrap text-right text-sm text-kx-muted md:table-cell md:px-4 md:py-3">
                      {ago(last_seen)}
                    </td>
                    <td className="kx-num hidden whitespace-nowrap text-right text-sm text-kx-muted md:table-cell md:px-4 md:py-3">
                      {link.expires_at ? localDate(link.expires_at) : "Never"}
                    </td>
                    <td className="col-start-3 row-span-2 row-start-1 md:py-2 md:pl-2 md:pr-3">
                      <div className="flex items-center justify-end gap-0.5">
                        <IconButton
                          data-testid={`copy-${link.code}`}
                          onClick={() => copy(link.code)}
                          label={`Copy short URL for /${link.code}`}
                        >
                          {copiedCode === link.code ? (
                            <Check size={15} aria-hidden="true" />
                          ) : (
                            <Copy size={15} aria-hidden="true" />
                          )}
                        </IconButton>
                        <RowMenu link={link} onToggle={toggle} onDelete={setPending} />
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {pages > 1 && (
              <nav
                aria-label="Links pagination"
                className="flex items-center justify-between border-t border-kx-line px-4 py-2 text-xs text-kx-muted"
              >
                <span className="kx-num" data-testid="links-page-info">
                  {current * PAGE_SIZE + 1}–{Math.min(filtered.length, (current + 1) * PAGE_SIZE)}{" "}
                  of {filtered.length}
                </span>
                <div className="flex items-center gap-1">
                  <IconButton
                    label="Previous page"
                    disabled={current === 0}
                    onClick={() => setPage(current - 1)}
                    data-testid="links-prev"
                  >
                    <ChevronLeft size={15} aria-hidden="true" />
                  </IconButton>
                  <span className="kx-num px-1">
                    Page {current + 1} of {pages}
                  </span>
                  <IconButton
                    label="Next page"
                    disabled={current >= pages - 1}
                    onClick={() => setPage(current + 1)}
                    data-testid="links-next"
                  >
                    <ChevronRight size={15} aria-hidden="true" />
                  </IconButton>
                </div>
              </nav>
            )}
          </>
        )}
      </Panel>

      <ConfirmDialog
        open={!!pending}
        title="Delete this link?"
        confirmLabel="Delete link"
        busy={deleting}
        onCancel={() => setPending(null)}
        onConfirm={() => remove(pending)}
      >
        <code className="font-mono text-kx-ink">/{pending}</code> will stop redirecting. This
        can&apos;t be undone.
      </ConfirmDialog>
    </div>
  );
}
