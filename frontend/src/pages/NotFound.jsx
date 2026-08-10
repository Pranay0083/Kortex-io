import { Link, useLocation } from "react-router-dom";
import { ArrowLeft } from "lucide-react";
import Logo from "@/components/Logo";
import ThemeToggle from "@/components/ThemeToggle";

const BTN = "kx-btn inline-flex h-9 items-center justify-center gap-2 whitespace-nowrap px-4";

/**
 * 404 for unknown routes and unknown/disabled/expired short codes.
 * `embedded` renders inside the console shell instead of full-screen.
 */
export default function NotFound({ embedded = false, title = "Page not found.", message }) {
  const { pathname } = useLocation();
  const body = (
    <div className="max-w-lg" data-testid="not-found-page">
      <h1 className="text-2xl font-semibold text-kx-ink sm:text-3xl">{title}</h1>
      <p className="mt-3 text-[15px] leading-relaxed text-kx-ink2">
        {message || (
          <>
            There&apos;s no page at{" "}
            <code className="break-all font-mono text-sm text-kx-ink">{pathname}</code>. The link
            may be old, or the address mistyped.
          </>
        )}
      </p>
      <div className="mt-6 flex flex-wrap gap-2">
        <Link
          to={embedded ? "/app" : "/"}
          className={`${BTN} bg-kx-accent text-kx-on-accent hover:bg-kx-accent/90`}
        >
          <ArrowLeft size={15} aria-hidden="true" /> {embedded ? "Back to overview" : "Go home"}
        </Link>
        {!embedded && (
          <Link
            to="/app"
            className={`${BTN} border border-kx-line2 bg-kx-surface text-kx-ink hover:bg-kx-hover`}
          >
            Open console
          </Link>
        )}
      </div>
    </div>
  );

  if (embedded) return <div className="py-10">{body}</div>;
  return (
    <div className="flex min-h-screen flex-col bg-kx-bg text-kx-ink">
      <header className="mx-auto flex w-full max-w-[1200px] items-center justify-between px-4 py-4 sm:px-6 lg:px-10">
        <Logo />
        <ThemeToggle />
      </header>
      <main className="mx-auto flex w-full max-w-[1200px] flex-1 items-center px-4 pb-24 sm:px-6 lg:px-10">
        {body}
      </main>
    </div>
  );
}
