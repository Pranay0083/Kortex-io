import { NavLink, Outlet, useNavigate, Link } from "react-router-dom";
import { useAuth } from "@/context/AuthContext";
import { Activity, BarChart3, Gauge, Link2, LogOut, Server, FlaskConical, Network } from "lucide-react";

const nav = [
  { to: "/app", end: true, label: "Overview", icon: BarChart3, testid: "nav-overview" },
  { to: "/app/links", label: "Links", icon: Link2, testid: "nav-links" },
  { to: "/app/bench", label: "Benchmark", icon: Gauge, testid: "nav-bench" },
  { to: "/app/ops", label: "Infra", icon: Server, testid: "nav-ops" },
];

const external = [
  { to: "/lab", label: "Design Lab", icon: FlaskConical, testid: "nav-lab" },
  { to: "/architecture", label: "Architecture", icon: Network, testid: "nav-architecture" },
];

export default function Shell() {
  const { user, logout } = useAuth();
  const navigate = useNavigate();

  const linkCls = ({ isActive }) =>
    `group flex items-center gap-3 border-l-2 px-4 py-2.5 font-mono text-[12px] uppercase tracking-wider transition-colors ${
      isActive
        ? "border-l-kx-cyan bg-kx-hover text-white"
        : "border-l-transparent text-zinc-500 hover:border-l-zinc-700 hover:text-zinc-200"
    }`;

  return (
    <div className="flex min-h-screen bg-kx-bg">
      <aside className="sticky top-0 hidden h-screen w-56 shrink-0 flex-col border-r border-kx-line bg-kx-surface lg:flex">
        <Link to="/" className="flex items-center gap-2 border-b border-kx-line px-5 py-5" data-testid="shell-logo">
          <div className="h-3 w-3 bg-kx-cyan" />
          <span className="font-display text-lg font-black tracking-tighter text-white">KORTEX</span>
        </Link>

        <nav className="mt-4 flex flex-col">
          <div className="kx-label px-4 pb-2">Console</div>
          {nav.map((n) => (
            <NavLink key={n.to} to={n.to} end={n.end} className={linkCls} data-testid={n.testid}>
              <n.icon size={15} strokeWidth={1.5} />
              {n.label}
            </NavLink>
          ))}
          <div className="kx-label px-4 pb-2 pt-6">Deep dive</div>
          {external.map((n) => (
            <NavLink key={n.to} to={n.to} className={linkCls} data-testid={n.testid}>
              <n.icon size={15} strokeWidth={1.5} />
              {n.label}
            </NavLink>
          ))}
        </nav>

        <div className="mt-auto border-t border-kx-line p-4">
          <div className="flex items-center gap-2 font-mono text-[11px] text-zinc-500">
            <Activity size={12} className="text-kx-green" strokeWidth={2} />
            node kortex-edge-01
          </div>
          <div className="mt-3 truncate font-mono text-[11px] text-zinc-400" data-testid="shell-user-email">
            {user?.email}
          </div>
          <button
            data-testid="logout-btn"
            onClick={async () => {
              await logout();
              navigate("/");
            }}
            className="kx-btn mt-3 flex w-full items-center justify-center gap-2 border border-kx-line py-2 text-zinc-400 hover:border-kx-red/50 hover:text-kx-red"
          >
            <LogOut size={12} strokeWidth={2} /> Sign out
          </button>
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-40 flex items-center gap-4 border-b border-kx-line bg-black/85 px-4 py-3 backdrop-blur-xl lg:hidden">
          <Link to="/" className="flex items-center gap-2">
            <div className="h-2.5 w-2.5 bg-kx-cyan" />
            <span className="font-display text-base font-black tracking-tighter">KORTEX</span>
          </Link>
          <nav className="flex gap-1 overflow-x-auto">
            {[...nav, ...external].map((n) => (
              <NavLink
                key={n.to}
                to={n.to}
                end={n.end}
                data-testid={`m-${n.testid}`}
                className={({ isActive }) =>
                  `whitespace-nowrap border px-2.5 py-1 font-mono text-[10px] uppercase tracking-wider ${
                    isActive ? "border-kx-cyan text-kx-cyan" : "border-kx-line text-zinc-500"
                  }`
                }
              >
                {n.label}
              </NavLink>
            ))}
          </nav>
        </header>
        <main className="min-w-0 flex-1 p-4 lg:p-7">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
