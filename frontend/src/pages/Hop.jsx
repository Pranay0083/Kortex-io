import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { api, shortUrl } from "@/lib/api";
import NotFound from "@/pages/NotFound";
import { Mark } from "@/components/Logo";

/**
 * In production the Go server answers GET /{code} itself with a 301/302, so this page is
 * only reached in `yarn dev` (Vite owns the origin) or when the server didn't recognise
 * the code and fell through to the SPA. Ask the resolver before bouncing so unknown,
 * disabled or expired codes get a proper page instead of a redirect loop.
 */
export default function Hop() {
  const { code } = useParams();
  const [state, setState] = useState({ code: null, status: "resolving" });
  const status = state.code === code ? state.status : "resolving";

  useEffect(() => {
    let alive = true;
    api
      .get(`/resolve/${encodeURIComponent(code)}`)
      .then(({ data }) => {
        if (!alive) return;
        const expired = data?.expires_at && Number(data.expires_at) <= Date.now();
        if (data?.active === false) setState({ code, status: "disabled" });
        else if (expired || data?.expired) setState({ code, status: "expired" });
        else window.location.replace(shortUrl(code));
      })
      .catch(() => alive && setState({ code, status: "missing" }));
    return () => {
      alive = false;
    };
  }, [code]);

  if (status !== "resolving") {
    const msg = {
      missing:
        "isn't a short link we know about. It may have been deleted, or the code is mistyped.",
      disabled: "exists, but its owner has turned it off.",
      expired: "has expired and no longer redirects.",
    }[status];
    return (
      <NotFound
        title={status === "missing" ? "Link not found." : `Link ${status}.`}
        message={
          <>
            <code className="break-all font-mono text-sm text-kx-ink">/{code}</code> {msg}
          </>
        }
      />
    );
  }

  return (
    <div className="flex h-screen items-center justify-center bg-kx-bg" data-testid="hop-page">
      <div className="flex flex-col items-center text-center" role="status">
        <Mark size={28} className="animate-kx-pulse" />
        <div className="mt-4 text-sm text-kx-muted">Opening link</div>
        <div className="mt-1 font-mono text-lg font-medium text-kx-ink">/{code}</div>
      </div>
    </div>
  );
}
