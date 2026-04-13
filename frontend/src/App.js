import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { Toaster } from "sonner";
import { AuthProvider, useAuth } from "@/context/AuthContext";
import Landing from "@/pages/Landing";
import Login from "@/pages/Login";
import Register from "@/pages/Register";
import Shell from "@/components/Shell";
import Dashboard from "@/pages/Dashboard";
import Links from "@/pages/Links";
import LinkDetail from "@/pages/LinkDetail";
import Bench from "@/pages/Bench";
import Ops from "@/pages/Ops";
import Lab from "@/pages/Lab";
import Architecture from "@/pages/Architecture";
import Hop from "@/pages/Hop";

const Booting = () => (
  <div className="flex h-screen items-center justify-center bg-kx-bg">
    <div className="font-mono text-xs tracking-[0.3em] text-kx-cyan animate-kx-pulse">
      KORTEX · BOOTING
    </div>
  </div>
);

function Guard({ children }) {
  const { user, ready } = useAuth();
  if (!ready) return <Booting />;
  if (!user) return <Navigate to="/login" replace />;
  return children;
}

function PublicOnly({ children }) {
  const { user, ready } = useAuth();
  if (!ready) return <Booting />;
  if (user) return <Navigate to="/app" replace />;
  return children;
}

function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <Toaster
          theme="dark"
          position="bottom-right"
          toastOptions={{
            style: {
              background: "#0A0A0A",
              border: "1px solid #222",
              borderRadius: 0,
              fontFamily: "JetBrains Mono, monospace",
              fontSize: "12px",
            },
          }}
        />
        <Routes>
          <Route path="/" element={<Landing />} />
          <Route path="/architecture" element={<Architecture />} />
          <Route path="/lab" element={<Lab />} />
          <Route
            path="/login"
            element={
              <PublicOnly>
                <Login />
              </PublicOnly>
            }
          />
          <Route
            path="/register"
            element={
              <PublicOnly>
                <Register />
              </PublicOnly>
            }
          />
          <Route
            path="/app"
            element={
              <Guard>
                <Shell />
              </Guard>
            }
          >
            <Route index element={<Dashboard />} />
            <Route path="links" element={<Links />} />
            <Route path="links/:code" element={<LinkDetail />} />
            <Route path="bench" element={<Bench />} />
            <Route path="ops" element={<Ops />} />
          </Route>
          <Route path="/:code" element={<Hop />} />
        </Routes>
      </AuthProvider>
    </BrowserRouter>
  );
}

export default App;
