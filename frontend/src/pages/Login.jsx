import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { useAuth } from "@/context/AuthContext";
import { apiError } from "@/lib/api";
import { Button } from "@/components/kx";
import { ArrowLeft, Terminal } from "lucide-react";

export function AuthFrame({ title, kicker, children, footer }) {
  return (
    <div className="relative flex min-h-screen bg-kx-bg">
      <div className="kx-grid absolute inset-0 opacity-60" />
      <div className="relative z-10 hidden w-1/2 flex-col justify-between border-r border-kx-line bg-kx-surface p-12 lg:flex">
        <Link to="/" className="flex items-center gap-2" data-testid="auth-logo">
          <div className="h-3 w-3 bg-kx-cyan" />
          <span className="font-display text-xl font-black tracking-tighter">KORTEX</span>
        </Link>
        <div>
          <div className="kx-label">System of record</div>
          <p className="mt-4 max-w-md font-display text-3xl font-black leading-tight tracking-tighter">
            Every short code is a Snowflake ID, base62-encoded, routed to one of four Postgres shards
            through a consistent hash ring.
          </p>
          <div className="mt-8 space-y-2 font-mono text-[11px] text-zinc-500">
            <div className="flex gap-3">
              <span className="text-kx-cyan">→</span> POST /shorten · id_gen → base62 → shard → warm redis
            </div>
            <div className="flex gap-3">
              <span className="text-kx-green">→</span> GET /:code · redis hit → 302 → async click event
            </div>
            <div className="flex gap-3">
              <span className="text-kx-yellow">→</span> consumer → columnar store → materialized rollups
            </div>
          </div>
        </div>
        <div className="flex items-center gap-2 font-mono text-[11px] text-zinc-600">
          <Terminal size={13} strokeWidth={1.5} /> go1.24 · postgres 15 · redis 7 · single node
        </div>
      </div>

      <div className="relative z-10 flex w-full items-center justify-center p-6 lg:w-1/2">
        <div className="w-full max-w-sm">
          <Link
            to="/"
            className="mb-8 inline-flex items-center gap-2 font-mono text-[11px] uppercase tracking-widest text-zinc-500 transition-colors hover:text-kx-cyan lg:hidden"
          >
            <ArrowLeft size={12} /> Kortex
          </Link>
          <div className="kx-label">{kicker}</div>
          <h1 className="mt-2 font-display text-4xl font-black tracking-tighter">{title}</h1>
          {children}
          <div className="mt-6 font-mono text-[11px] text-zinc-500">{footer}</div>
        </div>
      </div>
    </div>
  );
}

export function Field({ label, ...props }) {
  return (
    <label className="block">
      <span className="kx-label">{label}</span>
      <input {...props} className="kx-input mt-2 w-full px-3 py-2.5" />
    </label>
  );
}

export default function Login() {
  const { login } = useAuth();
  const navigate = useNavigate();
  const [email, setEmail] = useState("admin@kortex.dev");
  const [password, setPassword] = useState("kortex2026");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  const submit = async (e) => {
    e.preventDefault();
    setBusy(true);
    setErr("");
    try {
      await login(email, password);
      toast.success("Session established");
      navigate("/app", { replace: true });
    } catch (ex) {
      const m = apiError(ex, "Login failed");
      setErr(m);
      toast.error(m);
    }
    setBusy(false);
  };

  return (
    <AuthFrame
      kicker="Authenticate"
      title="Console access"
      footer={
        <>
          No account?{" "}
          <Link to="/register" className="text-kx-cyan hover:underline" data-testid="to-register">
            Create one
          </Link>
        </>
      }
    >
      <form onSubmit={submit} className="mt-7 space-y-4" data-testid="login-form">
        <Field
          label="Email"
          type="email"
          required
          value={email}
          data-testid="login-email"
          onChange={(e) => setEmail(e.target.value)}
        />
        <Field
          label="Password"
          type="password"
          required
          value={password}
          data-testid="login-password"
          onChange={(e) => setPassword(e.target.value)}
        />
        {err && (
          <div
            data-testid="login-error"
            className="border border-kx-red/40 bg-kx-red/5 px-3 py-2 font-mono text-[11px] text-kx-red"
          >
            {err}
          </div>
        )}
        <Button type="submit" disabled={busy} className="w-full py-3" data-testid="login-submit">
          {busy ? "Authenticating…" : "Sign in"}
        </Button>
      </form>
      <div className="mt-4 border border-kx-line bg-kx-surface px-3 py-2 font-mono text-[10px] leading-relaxed text-zinc-500">
        demo account pre-filled · admin@kortex.dev / kortex2026
        <br />
        14 days of seeded click history, 180k rows
      </div>
    </AuthFrame>
  );
}
