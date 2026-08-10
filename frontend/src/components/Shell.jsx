import { useState } from "react";
import { NavLink, Outlet, useLocation, useNavigate } from "react-router-dom";
import { useAuth } from "@/context/AuthContext";
import { useSystemInfo } from "@/hooks/useSystemInfo";
import { BarChart3, Gauge, Link2, LogOut, Menu, Server, X } from "lucide-react";
import Logo from "@/components/Logo";
import ThemeToggle from "@/components/ThemeToggle";

const nav = [
  { to: "/app", end: true, label: "Overview", icon: BarChart3, testid: "nav-overview" },
  { to: "/app/links", label: "Links", icon: Link2, testid: "nav-links" },
];

const underTheHood = [
  { to: "/app/ops", label: "Infrastructure", icon: Server, testid: "nav-ops" },
  { to: "/app/bench", label: "Benchmark", icon: Gauge, testid: "nav-bench", admin: true },
];

const linkCls = ({ isActive }) =>
  `flex items-center gap-2.5 rounded-control px-2.5 py-1.5 text-sm transition-colors ${
    isActive
      ? "bg-kx-hover font-medium text-kx-ink"
      : "text-kx-muted hover:bg-kx-hover/60 hover:text-kx-ink"
  }`;

function NavGroups({ prefix = "", isAdmin }) {
  return (
    <>
      <div className="flex flex-col gap-0.5">
        {nav.map((n) => (
          <NavLink
            key={n.to}
            to={n.to}
            end={n.end}
            className={linkCls}
            data-testid={prefix + n.testid}
          >
            <n.icon size={16} strokeWidth={1.6} aria-hidden="true" />
            {n.label}
          </NavLink>
        ))}
      </div>
      <div className="mb-1.5 mt-6 px-2.5 text-xs font-medium text-kx-faint">Under the hood</div>
      <div className="flex flex-col gap-0.5">
        {underTheHood
          .filter((n) => isAdmin || !n.admin)
          .map((n) => (
            <NavLink key={n.to} to={n.to} className={linkCls} data-testid={prefix + n.testid}>
              <n.icon size={16} strokeWidth={1.6} aria-hidden="true" />
              {n.label}
            </NavLink>
          ))}
      </div>
    </>
  );
}

export default function Shell() {
  const { user, logout } = useAuth();
  const { nodeName } = useSystemInfo();
  const navigate = useNavigate();
  const location = useLocation();
  // The phone menu is open for the path it was opened on, so navigating closes it.
  const [menuPath, setMenuPath] = useState(null);
  const menuOpen = menuPath === location.pathname;

  const signOut = async () => {
    await logout();
    navigate("/");
  };

  return (
    <div className="flex min-h-screen bg-kx-bg">
      <aside
        aria-label="Console navigation"
        className="sticky top-0 hidden h-screen w-60 shrink-0 flex-col border-r border-kx-line bg-kx-bg px-3 py-4 lg:flex"
      >
        <div className="px-2.5 pb-5">
          <Logo testid="shell-logo" />
        </div>

        <nav className="flex flex-col">
          <NavGroups isAdmin={user?.role === "admin"} />
        </nav>

        <div className="mt-auto border-t border-kx-line px-2.5 pt-4">
          <div className="flex items-center gap-1.5 text-xs text-kx-muted">
            <span className="h-1.5 w-1.5 rounded-full bg-kx-positive" aria-hidden="true" />
            {nodeName || "—"}
          </div>
          <div
            className="mt-1 truncate text-sm text-kx-ink"
            data-testid="shell-user-email"
            title={user?.email}
          >
            {user?.email}
          </div>
          {user?.role === "demo" && (
            <div className="text-xs text-kx-muted" data-testid="shell-demo-note">
              Shared demo account
            </div>
          )}
          <div className="mt-3 flex items-center gap-2">
            <button
              type="button"
              data-testid="logout-btn"
              onClick={signOut}
              className="kx-btn inline-flex h-8 flex-1 items-center justify-center gap-2 rounded-control border border-kx-line text-kx-muted hover:bg-kx-hover hover:text-kx-ink"
            >
              <LogOut size={14} strokeWidth={1.75} aria-hidden="true" /> Sign out
            </button>
            <ThemeToggle />
          </div>
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-40 border-b border-kx-line bg-kx-bg lg:hidden">
          <div className="flex h-12 items-center gap-2 px-4">
            <Logo size={20} />
            <span
              className="ml-auto hidden min-w-0 truncate text-xs text-kx-muted sm:block"
              data-testid="m-shell-user-email"
            >
              {user?.email}
            </span>
            <ThemeToggle className="ml-auto sm:ml-0" />
            <button
              type="button"
              onClick={() => setMenuPath(menuOpen ? null : location.pathname)}
              aria-expanded={menuOpen}
              aria-controls="m-nav"
              aria-label={menuOpen ? "Close menu" : "Open menu"}
              data-testid="m-menu-btn"
              className="inline-flex h-8 w-8 items-center justify-center rounded-control border border-kx-line text-kx-muted hover:text-kx-ink"
            >
              {menuOpen ? (
                <X size={16} aria-hidden="true" />
              ) : (
                <Menu size={16} aria-hidden="true" />
              )}
            </button>
          </div>
          {menuOpen && (
            <nav
              id="m-nav"
              aria-label="Console navigation"
              className="border-t border-kx-line bg-kx-bg px-3 pb-4 pt-3"
            >
              <NavGroups prefix="m-" isAdmin={user?.role === "admin"} />
              <button
                type="button"
                onClick={signOut}
                data-testid="m-logout-btn"
                className="kx-btn mt-5 inline-flex h-10 w-full items-center justify-center gap-2 rounded-control border border-kx-line text-kx-muted hover:text-kx-ink"
              >
                <LogOut size={14} strokeWidth={1.75} aria-hidden="true" /> Sign out
              </button>
            </nav>
          )}
        </header>
        <main className="mx-auto w-full min-w-0 max-w-[1320px] flex-1 px-4 py-6 lg:px-8 lg:py-8">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
