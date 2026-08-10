import { afterEach, describe, expect, it } from "vitest";
import { useEffect } from "react";
import { act, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { AuthProvider, useAuth } from "@/context/AuthContext";
import { Guard, PublicOnly } from "@/components/Guards";
import { api } from "@/lib/api";
import { countCalls, mockApi } from "@/test/mockApi";

const USER = { id: "1", email: "admin@kortex.dev", role: "admin" };

// Latest context value, captured after render for imperative calls (login/logout).
const ctx = { current: null };
function Probe() {
  const auth = useAuth();
  useEffect(() => {
    ctx.current = auth;
  });
  if (!auth.ready) return <div>booting</div>;
  return <div>{auth.user ? `user:${auth.user.email}` : "logged-out"}</div>;
}

const renderAuth = (ui = <Probe />, entries = ["/"]) =>
  render(
    <MemoryRouter initialEntries={entries}>
      <AuthProvider>{ui}</AuthProvider>
    </MemoryRouter>,
  );

describe("AuthContext boot", () => {
  let calls;
  afterEach(() => calls?.restore());

  it("me 200 → user is set", async () => {
    calls = mockApi({ "GET /auth/me": [200, USER] });
    renderAuth();
    expect(await screen.findByText("user:admin@kortex.dev")).toBeInTheDocument();
    expect(countCalls(calls, "POST /auth/refresh")).toBe(0);
  });

  it("me 401 and no refresh cookie → logged out, me is not retried", async () => {
    calls = mockApi({
      "GET /auth/me": [401, { detail: "unauthorized" }],
      "POST /auth/refresh": [401, { detail: "no refresh token" }],
    });
    renderAuth();
    expect(await screen.findByText("logged-out")).toBeInTheDocument();
    expect(countCalls(calls, "GET /auth/me")).toBe(1);
    expect(countCalls(calls, "POST /auth/refresh")).toBe(1);
  });

  it("me 401 but refresh cookie valid → user restored from the refresh response", async () => {
    calls = mockApi({
      "GET /auth/me": [401, {}],
      "POST /auth/refresh": [200, { user: USER }],
    });
    renderAuth();
    expect(await screen.findByText("user:admin@kortex.dev")).toBeInTheDocument();
  });

  it("network error on boot → logged out without trying refresh", async () => {
    calls = mockApi({ "GET /auth/me": [503, {}] });
    renderAuth();
    expect(await screen.findByText("logged-out")).toBeInTheDocument();
    expect(countCalls(calls, "POST /auth/refresh")).toBe(0);
  });

  it("login stores the user from { user } and logout clears it", async () => {
    calls = mockApi({
      "GET /auth/me": [401, {}],
      "POST /auth/refresh": [401, {}],
      "POST /auth/login": [200, { user: USER }],
      "POST /auth/logout": [200, { ok: true }],
    });
    renderAuth();
    await screen.findByText("logged-out");
    await act(() => ctx.current.login("admin@kortex.dev", "pw"));
    expect(screen.getByText("user:admin@kortex.dev")).toBeInTheDocument();
    expect(JSON.parse(calls.find((c) => c.key === "POST /auth/login").data)).toEqual({
      email: "admin@kortex.dev",
      password: "pw",
    });
    await act(() => ctx.current.logout());
    expect(screen.getByText("logged-out")).toBeInTheDocument();
    expect(countCalls(calls, "POST /auth/logout")).toBe(1);
  });

  it("never touches localStorage", async () => {
    calls = mockApi({ "GET /auth/me": [200, USER] });
    localStorage.setItem("kortex_token", "stale");
    renderAuth();
    await screen.findByText("user:admin@kortex.dev");
    const req = calls.find((c) => c.key === "GET /auth/me");
    expect(req.config.headers.Authorization).toBeUndefined();
    localStorage.clear();
  });

  it("a failed refresh mid-session logs out and sends the user to /login", async () => {
    let expired = false;
    calls = mockApi({
      "GET /auth/me": [200, USER],
      "GET /links": [401, {}],
      "POST /auth/refresh": () => {
        expired = true;
        return [401, {}];
      },
    });
    render(
      <MemoryRouter initialEntries={["/app"]}>
        <AuthProvider>
          <Routes>
            <Route path="/login" element={<div>login page</div>} />
            <Route path="/app" element={<Probe />} />
          </Routes>
        </AuthProvider>
      </MemoryRouter>,
    );
    await screen.findByText("user:admin@kortex.dev");
    await act(async () => {
      await api.get("/links").catch(() => {});
    });
    expect(expired).toBe(true);
    expect(await screen.findByText("login page")).toBeInTheDocument();
  });
});

describe("Guard", () => {
  let calls;
  afterEach(() => calls?.restore());

  const routes = (
    <Routes>
      <Route
        path="/login"
        element={
          <PublicOnly>
            <div>login page</div>
          </PublicOnly>
        }
      />
      <Route path="/app" element={<div>overview</div>} />
      <Route
        path="/app/links"
        element={
          <Guard>
            <div>secret links</div>
          </Guard>
        }
      />
    </Routes>
  );

  it("redirects unauthenticated users to /login", async () => {
    calls = mockApi({ "GET /auth/me": [401, {}], "POST /auth/refresh": [401, {}] });
    renderAuth(routes, ["/app/links"]);
    expect(await screen.findByText("login page")).toBeInTheDocument();
    expect(screen.queryByText("secret links")).not.toBeInTheDocument();
  });

  it("renders protected content for a signed-in user", async () => {
    calls = mockApi({ "GET /auth/me": [200, USER] });
    renderAuth(routes, ["/app/links"]);
    expect(await screen.findByText("secret links")).toBeInTheDocument();
  });

  it("shows the boot screen until /auth/me answers", async () => {
    let answer;
    calls = mockApi({ "GET /auth/me": () => new Promise((r) => (answer = r)) });
    renderAuth(routes, ["/app/links"]);
    expect(screen.getByRole("status")).toHaveTextContent(/Loading/);
    await waitFor(() => expect(answer).toBeTypeOf("function"));
    await act(async () => answer([200, USER]));
    expect(await screen.findByText("secret links")).toBeInTheDocument();
  });
});
