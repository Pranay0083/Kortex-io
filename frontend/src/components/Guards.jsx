import { Navigate, useLocation } from "react-router-dom";
import { useAuth } from "@/context/AuthContext";

export const Booting = ({ label = "Loading…" }) => (
  <div className="flex h-screen items-center justify-center bg-kx-bg" role="status">
    <div className="animate-kx-pulse text-sm text-kx-muted">{label}</div>
  </div>
);

/** Only render children for an authenticated user; otherwise bounce to /login. */
export function Guard({ children }) {
  const { user, ready } = useAuth();
  const location = useLocation();
  if (!ready) return <Booting />;
  if (!user) return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  return children;
}

/** Login/register: already signed in → straight to the console. */
export function PublicOnly({ children }) {
  const { user, ready } = useAuth();
  const location = useLocation();
  if (!ready) return <Booting />;
  if (user) {
    const from = location.state?.from;
    return (
      <Navigate to={typeof from === "string" && from.startsWith("/app") ? from : "/app"} replace />
    );
  }
  return children;
}

/** Admin-only console screens (the load generator): everyone else lands on the overview. */
export function AdminOnly({ children }) {
  const { user } = useAuth();
  if (user?.role !== "admin") return <Navigate to="/app" replace />;
  return children;
}
