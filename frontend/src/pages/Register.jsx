import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { useAuth } from "@/context/AuthContext";
import { apiError } from "@/lib/api";
import { Button } from "@/components/kx";
import { AuthFrame, Field, FormError } from "@/pages/Login";

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
      toast.success("Account created");
      navigate("/app", { replace: true });
    } catch (ex) {
      const m = apiError(ex, "Couldn't create the account");
      setErr(m);
      toast.error(m);
    }
    setBusy(false);
  };

  return (
    <AuthFrame
      title="Create your account"
      sub="Your links, with click analytics from the first click."
      footer={
        <>
          Already have an account?{" "}
          <Link
            to="/login"
            className="font-medium text-kx-accent hover:underline"
            data-testid="to-login"
          >
            Sign in
          </Link>
        </>
      }
    >
      <form onSubmit={submit} className="mt-7 space-y-4" data-testid="register-form">
        <Field
          label="Name"
          autoComplete="name"
          value={form.name}
          data-testid="register-name"
          onChange={set("name")}
        />
        <Field
          label="Email"
          type="email"
          autoComplete="email"
          required
          value={form.email}
          data-testid="register-email"
          onChange={set("email")}
        />
        <Field
          label="Password"
          hint="At least 8 characters."
          type="password"
          autoComplete="new-password"
          required
          minLength={8}
          value={form.password}
          data-testid="register-password"
          onChange={set("password")}
        />
        {err && <FormError testid="register-error">{err}</FormError>}
        <Button
          type="submit"
          size="lg"
          disabled={busy}
          className="w-full"
          data-testid="register-submit"
        >
          {busy ? "Creating account…" : "Create account"}
        </Button>
      </form>
    </AuthFrame>
  );
}
