import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import { usePolling } from "@/hooks/usePolling";

let hidden = false;

function setHidden(v) {
  hidden = v;
  document.dispatchEvent(new Event("visibilitychange"));
}

describe("usePolling", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    hidden = false;
    vi.spyOn(document, "hidden", "get").mockImplementation(() => hidden);
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("fetches immediately and then on every interval", () => {
    const fn = vi.fn();
    renderHook(() => usePolling(fn, 1000));
    expect(fn).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(3000);
    expect(fn).toHaveBeenCalledTimes(4);
  });

  it("pauses while the tab is hidden and refetches when it becomes visible", () => {
    const fn = vi.fn();
    renderHook(() => usePolling(fn, 1000));
    expect(fn).toHaveBeenCalledTimes(1);

    setHidden(true);
    vi.advanceTimersByTime(10_000);
    expect(fn).toHaveBeenCalledTimes(1);

    setHidden(false);
    expect(fn).toHaveBeenCalledTimes(2); // immediate catch-up fetch
    vi.advanceTimersByTime(1000);
    expect(fn).toHaveBeenCalledTimes(3);
  });

  it("does not start while hidden", () => {
    hidden = true;
    const fn = vi.fn();
    renderHook(() => usePolling(fn, 1000));
    vi.advanceTimersByTime(5000);
    expect(fn).not.toHaveBeenCalled();
  });

  it("respects enabled=false and stops on unmount", () => {
    const fn = vi.fn();
    const { rerender, unmount } = renderHook(({ on }) => usePolling(fn, 1000, { enabled: on }), {
      initialProps: { on: false },
    });
    vi.advanceTimersByTime(3000);
    expect(fn).not.toHaveBeenCalled();
    rerender({ on: true });
    expect(fn).toHaveBeenCalledTimes(1);
    unmount();
    vi.advanceTimersByTime(5000);
    expect(fn).toHaveBeenCalledTimes(1);
  });

  it("restarts with the new interval when ms changes", () => {
    const fn = vi.fn();
    const { rerender } = renderHook(({ ms }) => usePolling(fn, ms), { initialProps: { ms: 5000 } });
    rerender({ ms: 500 });
    fn.mockClear();
    vi.advanceTimersByTime(1000);
    expect(fn).toHaveBeenCalledTimes(2);
  });
});
