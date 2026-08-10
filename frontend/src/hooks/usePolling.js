import { useEffect } from "react";

/**
 * Call `fn` immediately and then every `ms` while the tab is visible.
 * Pauses while `document.hidden` and refetches as soon as the tab is visible again.
 *
 * Pass a memoized callback (useCallback): when its identity changes (e.g. the selected
 * range changed) polling restarts with an immediate fetch.
 */
export function usePolling(fn, ms, { enabled = true } = {}) {
  useEffect(() => {
    if (!enabled) return undefined;
    let timer = null;

    const start = () => {
      if (timer === null && ms > 0) timer = setInterval(fn, ms);
    };
    const stop = () => {
      if (timer !== null) clearInterval(timer);
      timer = null;
    };
    const onVisibility = () => {
      if (document.hidden) {
        stop();
      } else {
        fn();
        start();
      }
    };

    if (!document.hidden) {
      fn();
      start();
    }
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      stop();
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [fn, ms, enabled]);
}
