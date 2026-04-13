import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { useAuth } from "@/context/AuthContext";
import { apiError } from "@/lib/api";
import { Button } from "@/components/kx";
import { AuthFrame, Field } from "@/pages/Login";

export default function Register() {
  const { register } = useAuth();
  const navigate = useNavigate();
  const [form, setForm] = useState({ name: "", email: "", password: "" });
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  const set = (k) => (e) => setForm({ ...form, [k]: e.target.value });

  const submit = async (e) => {
    e.preventDefault();
    setBusy(true);
    setErr("");
    try {
      await register(form.email, form.password, form.name);
      toast.success("Account provisioned");
      navigate("/app", { replace: true });
    } catch (ex) {
      const m = apiError(ex, "Registration failed");
      setErr(m);
      toast.error(m);
    }
    setBusy(false);
  };

  return (
    <AuthFrame
      kicker="Provision"
      title="New tenant"
      footer={
        <>
          Already registered?{" "}
          <Link to="/login" className="text-kx-cyan hover:underline" data-testid="to-login">
            Sign in
          </Link>
        </>
      }
    >
      <form onSubmit={submit} className="mt-7 space-y-4" data-testid="register-form">
        <Field label="Name" value={form.name} data-testid="register-name" onChange={set("name")} />
        <Field
          label="Email"
          type="email"
          required
          value={form.email}
          data-testid="register-email"
          onChange={set("email")}
        />
        <Field
          label="Password (8+ chars)"
          type="password"
          required
          minLength={8}
          value={form.password}
          data-testid="register-password"
          onChange={set("password")}
        />
        {err && (
          <div
            data-testid="register-error"
            className="border border-kx-red/40 bg-kx-red/5 px-3 py-2 font-mono text-[11px] text-kx-red"
          >
            {err}
          </div>
        )}
        <Button type="submit" disabled={busy} className="w-full py-3" data-testid="register-submit">
          {busy ? "Provisioning…" : "Create account"}
        </Button>
      </form>
      <div className="mt-4 border border-kx-line bg-kx-surface px-3 py-2 font-mono text-[10px] leading-relaxed text-zinc-500">
        bcrypt cost 11 · JWT HS256 · 40 links/min per-tenant rate limit
      </div>
    </AuthFrame>
  );
}
