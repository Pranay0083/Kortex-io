import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ago,
  api,
  apiError,
  compact,
  ORIGIN,
  prettyUrl,
  qrUrl,
  setSessionExpiredHandler,
  shortUrl,
} from "@/lib/api";
import { countCalls, mockApi } from "@/test/mockApi";

describe("compact()", () => {
  it.each([
    [null, "0"],
    [undefined, "0"],
    [0, "0"],
    [999, "999"],
    [12.6, "13"],
    [1500, "1.5k"],
    [12_345, "12k"],
    [2_500_000, "2.50M"],
    [3_210_000_000, "3.21B"],
  ])("compact(%s) = %s", (n, out) => {
    expect(compact(n)).toBe(out);
  });
});

describe("ago()", () => {
  const now = 1_700_000_000_000;
  it.each([
    [0, "—"],
    [null, "—"],
    [now - 5_000, "5s ago"],
    [now - 5 * 60_000, "5m ago"],
    [now - 3 * 3_600_000, "3h ago"],
    [now - 2 * 86_400_000, "2d ago"],
    [now + 10_000, "0s ago"], // clock skew never goes negative
  ])("ago(%s)", (ms, out) => {
    expect(ago(ms, now)).toBe(out);
  });
});

describe("URLs", () => {
  it("are absolute, built from the page origin when no backend URL is configured", () => {
    expect(ORIGIN).toBe(window.location.origin);
    expect(shortUrl("abc")).toBe(`${window.location.origin}/api/r/abc`);
    expect(prettyUrl("abc")).toBe(`${window.location.origin}/abc`);
    expect(qrUrl("abc")).toBe(
      `${window.location.origin}/api/qr/abc?url=${encodeURIComponent(`${window.location.origin}/abc`)}`,
    );
    expect(prettyUrl("abc")).toMatch(/^https?:\/\//);
  });

  it("sends cookies with every request", () => {
    expect(api.defaults.withCredentials).toBe(true);
  });
});

describe("apiError()", () => {
  it("prefers the server's detail message", () => {
    expect(apiError({ response: { data: { detail: "demo links are protected" } } })).toBe(
      "demo links are protected",
    );
    expect(apiError({ message: "Network Error" })).toBe("Network Error");
    expect(apiError({}, "fallback")).toBe("fallback");
  });
});

describe("401 → refresh → retry interceptor", () => {
  let calls;
  afterEach(() => {
    calls?.restore();
    setSessionExpiredHandler(null);
  });

  it("refreshes once and retries the original request", async () => {
    let authed = false;
    calls = mockApi({
      "GET /links": () => (authed ? [200, { items: [1] }] : [401, { detail: "expired" }]),
      "POST /auth/refresh": () => {
        authed = true;
        return [200, { user: { email: "a@b.c" } }];
      },
    });
    const { data } = await api.get("/links");
    expect(data).toEqual({ items: [1] });
    expect(countCalls(calls, "POST /auth/refresh")).toBe(1);
    expect(countCalls(calls, "GET /links")).toBe(2);
  });

  it("shares a single refresh between concurrent 401s", async () => {
    let authed = false;
    let release;
    const gate = new Promise((r) => (release = r));
    calls = mockApi({
      "GET /a": () => (authed ? [200, "a"] : [401, {}]),
      "GET /b": () => (authed ? [200, "b"] : [401, {}]),
      "GET /c": () => (authed ? [200, "c"] : [401, {}]),
      "POST /auth/refresh": async () => {
        await gate;
        authed = true;
        return [200, { user: {} }];
      },
    });
    const pending = Promise.all([api.get("/a"), api.get("/b"), api.get("/c")]);
    // let all three 401s land before the refresh resolves
    await new Promise((r) => setTimeout(r, 10));
    release();
    const res = await pending;
    expect(res.map((r) => r.data)).toEqual(["a", "b", "c"]);
    expect(countCalls(calls, "POST /auth/refresh")).toBe(1);
  });

  it("gives up and signals session expiry when refresh fails", async () => {
    const expired = vi.fn();
    setSessionExpiredHandler(expired);
    calls = mockApi({
      "GET /links": [401, { detail: "expired" }],
      "POST /auth/refresh": [401, { detail: "refresh token invalid" }],
    });
    await expect(api.get("/links")).rejects.toMatchObject({ response: { status: 401 } });
    expect(expired).toHaveBeenCalledTimes(1);
    expect(countCalls(calls, "GET /links")).toBe(1);
  });

  it("does not retry a request that already failed after a refresh", async () => {
    calls = mockApi({
      "GET /links": [401, {}],
      "POST /auth/refresh": [200, { user: {} }],
    });
    await expect(api.get("/links")).rejects.toMatchObject({ response: { status: 401 } });
    expect(countCalls(calls, "POST /auth/refresh")).toBe(1);
    expect(countCalls(calls, "GET /links")).toBe(2);
  });

  it.each(["/auth/login", "/auth/register"])("never refreshes for %s", async (path) => {
    calls = mockApi({
      [`POST ${path}`]: [401, { detail: "bad credentials" }],
      "POST /auth/refresh": [200, { user: {} }],
    });
    await expect(api.post(path, {})).rejects.toMatchObject({ response: { status: 401 } });
    expect(countCalls(calls, "POST /auth/refresh")).toBe(0);
  });

  it("does not recurse when /auth/refresh itself returns 401", async () => {
    calls = mockApi({ "POST /auth/refresh": [401, {}] });
    await expect(api.post("/auth/refresh")).rejects.toMatchObject({ response: { status: 401 } });
    expect(countCalls(calls, "POST /auth/refresh")).toBe(1);
  });

  it("leaves non-401 errors alone", async () => {
    calls = mockApi({ "DELETE /links/x": [403, { detail: "demo links are protected" }] });
    await expect(api.delete("/links/x")).rejects.toMatchObject({ response: { status: 403 } });
    expect(countCalls(calls, "POST /auth/refresh")).toBe(0);
  });
});
