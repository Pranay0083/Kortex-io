import { Component } from "react";
import { AlertTriangle, RotateCcw } from "lucide-react";
import { Mark } from "@/components/Logo";

/** Top-level crash screen: keeps a render bug from blanking the whole console. */
export default class ErrorBoundary extends Component {
  state = { error: null };

  static getDerivedStateFromError(error) {
    return { error };
  }

  componentDidCatch(error, info) {
    console.error("[kortex] render error", error, info?.componentStack);
  }

  render() {
    const { error } = this.state;
    if (!error) return this.props.children;
    return (
      <div className="flex min-h-screen flex-col bg-kx-bg text-kx-ink">
        {/* Rendered outside the router, so a plain anchor instead of <Logo>. */}
        <header className="mx-auto w-full max-w-[1200px] px-4 py-4 sm:px-6 lg:px-10">
          <a
            href="/"
            aria-label="Kortex home"
            className="inline-flex items-center gap-2 text-kx-ink"
          >
            <Mark />
            <span className="text-[15px] font-semibold tracking-tight">Kortex</span>
          </a>
        </header>
        <main className="flex flex-1 items-center justify-center px-4 pb-24">
          <section
            role="alert"
            data-testid="error-boundary"
            className="kx-panel w-full max-w-lg p-6"
          >
            <div className="flex items-center gap-2 text-sm font-medium text-kx-negative">
              <AlertTriangle size={16} strokeWidth={2} aria-hidden="true" />
              Unexpected error
            </div>
            <h1 className="mt-2 text-2xl font-semibold text-kx-ink">Something went wrong.</h1>
            <p className="mt-2 text-sm leading-relaxed text-kx-ink2">
              This page hit an error it couldn&apos;t recover from. Reloading usually fixes it. If
              it keeps happening, the message below is what to report.
            </p>
            <pre className="mt-4 max-h-40 overflow-auto whitespace-pre-wrap break-words rounded-control border border-kx-line bg-kx-card p-3 font-mono text-xs text-kx-ink2">
              {String(error?.message || error)}
            </pre>
            <div className="mt-5 flex flex-wrap gap-2">
              <button
                type="button"
                onClick={() => window.location.reload()}
                className="kx-btn inline-flex h-9 items-center gap-2 whitespace-nowrap bg-kx-accent px-4 text-kx-on-accent hover:bg-kx-accent/90"
              >
                <RotateCcw size={15} aria-hidden="true" /> Reload
              </button>
              <a
                href="/"
                className="kx-btn inline-flex h-9 items-center whitespace-nowrap border border-kx-line2 bg-kx-surface px-4 text-kx-ink hover:bg-kx-hover"
              >
                Go home
              </a>
            </div>
          </section>
        </main>
      </div>
    );
  }
}
