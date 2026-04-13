import { useEffect } from "react";
import { useParams } from "react-router-dom";
import { shortUrl } from "@/lib/api";

/** Pretty short links (/{code}) land here because the k8s ingress sends
 *  non-/api paths to the SPA. We bounce straight to the Go redirect handler. */
export default function Hop() {
  const { code } = useParams();
  useEffect(() => {
    window.location.replace(shortUrl(code));
  }, [code]);
  return (
    <div className="flex h-screen items-center justify-center bg-kx-bg" data-testid="hop-page">
      <div className="text-center">
        <div className="kx-label">Resolving short code</div>
        <div className="kx-num mt-3 text-3xl font-bold text-kx-cyan">/{code}</div>
        <div className="mt-4 h-px w-56 overflow-hidden bg-kx-line">
          <div className="h-full w-1/4 animate-kx-sweep bg-kx-cyan" />
        </div>
      </div>
    </div>
  );
}
