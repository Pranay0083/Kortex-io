import { useEffect, useState } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { useAuth } from "@/context/AuthContext";
import { api, apiError } from "@/lib/api";
import { Button } from "@/components/kx";
import Logo from "@/components/Logo";
import ThemeToggle from "@/components/ThemeToggle";

const POINTS = [
  "Custom aliases, expiry dates and permanent or temporary redirects.",
  "A QR code for every link, and one click to switch a link off.",
  "Clicks broken down by referrer, device and browser as they happen.",
];

export function AuthFrame({ title, sub, children, footer }) {
  return (
    <div className="flex min-h-screen bg-kx-bg">
      <aside className="hidden w-[44%] max-w-[600px] flex-col justify-between border-r border-kx-line bg-kx-surface p-12 lg:flex">
        <Logo size={24} testid="auth-logo" />
        <div className="max-w-md">
          <p className="text-[28px] font-semibold leading-snug text-kx-ink">
            Short links, with every click counted.
          </p>
          <ul className="mt-6 space-y-3 text-[15px] leading-relaxed text-kx-ink2">
            {POINTS.map((p) => (
              <li key={p} className="flex gap-3">
                <span
                  className="mt-[9px] h-1.5 w-1.5 flex-none rounded-full bg-kx-accent"
                  aria-hidden="true"
                />
                {p}
              </li>
            ))}
          </ul>
        </div>
        <p className="text-sm text-kx-muted">Built in Go with Postgres and Redis.</p>
      </aside>

      <main className="relative flex flex-1 flex-col">
        <div className="flex items-center justify-between px-4 py-4 sm:px-6 lg:justify-end">
          <Logo className="lg:hidden" />
          <ThemeToggle />
        </div>
        <div className="flex flex-1 items-start justify-center px-4 pb-16 pt-6 sm:items-center sm:pt-0">
          <div className="w-full max-w-sm">
            <h1 className="text-2xl font-semibold text-kx-ink">{title}</h1>
            {sub && <p className="mt-1.5 text-sm text-kx-muted">{sub}</p>}
            {children}
            <div className="mt-6 text-sm text-kx-muted">{footer}</div>
          </div>
        </div>
      </main>
    </div>
  );
}

export function Field({ label, hint, ...props }) {
  return (
    <label className="block">
      <span className="block text-sm font-medium text-kx-ink2">{label}</span>
      <input {...props} className="kx-input mt-1.5 h-10 w-full px-3" />
      {hint && <span className="mt-1.5 block text-xs text-kx-muted">{hint}</span>}
    </label>
  );
}

export const FormError = ({ children, testid }) => (
  <div
    role="alert"
    data-testid={testid}
    className="rounded-control border border-kx-negative/30 bg-kx-negative/5 px-3 py-2 text-sm text-kx-negative"
  >
    {children}
  </div>
);

export default function Login() {
  const { login, demoLogin } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [demo, setDemo] = useState(false);

  // "Try the demo" signs into a shared non-admin trial account; the server
  // only offers it while demo mode is on.
  useEffect(() => {
    let alive = true;
    api
      .get("/system/stats")
      .then(({ data }) => {
        if (alive) setDemo(data?.demo_mode === true);
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, []);

  const signIn = async (attempt) => {
    setBusy(true);
    setErr("");
    try {
      await attempt();
      toast.success("Signed in");
      const from = location.state?.from;
      navigate(typeof from === "string" && from.startsWith("/app") ? from : "/app", {
        replace: true,
      });
    } catch (ex) {
      const m = apiError(ex, "Sign-in failed");
      setErr(m);
      toast.error(m);
    }
    setBusy(false);
  };

  const submit = (e) => {
    e.preventDefault();
    signIn(() => login(email, password));
  };

  return (
    <AuthFrame
      title="Sign in to Kortex"
      sub="Manage your links and see who's clicking."
      footer={
        <>
          No account?{" "}
          <Link
            to="/register"
            className="font-medium text-kx-accent hover:underline"
            data-testid="to-register"
          >
            Create one
          </Link>
        </>
      }
    >
      <form onSubmit={submit} className="mt-7 space-y-4" data-testid="login-form">
        <Field
          label="Email"
          type="email"
          autoComplete="email"
          required
          value={email}
          data-testid="login-email"
          onChange={(e) => setEmail(e.target.value)}
        />
        <Field
          label="Password"
          type="password"
          autoComplete="current-password"
          required
          value={password}
          data-testid="login-password"
          onChange={(e) => setPassword(e.target.value)}
        />
        {err && <FormError testid="login-error">{err}</FormError>}
        <Button
          type="submit"
          size="lg"
          disabled={busy}
          className="w-full"
          data-testid="login-submit"
        >
          {busy ? "Signing in…" : "Sign in"}
        </Button>
      </form>
      {demo && (
        <div className="mt-3" data-testid="login-demo">
          <Button
            variant="secondary"
            size="lg"
            disabled={busy}
            className="w-full"
            data-testid="login-demo-btn"
            onClick={() => signIn(demoLogin)}
          >
            Try the demo
          </Button>
          <p className="mt-2 text-center text-xs text-kx-muted">
            A shared trial account. Sample links are read-only; create your own to edit.
          </p>
        </div>
      )}
    </AuthFrame>
  );
}
