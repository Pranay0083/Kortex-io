import { lazy, Suspense } from "react";
import { BrowserRouter, Routes, Route } from "react-router-dom";
import { Toaster } from "sonner";
import { AuthProvider } from "@/context/AuthContext";
import { ThemeProvider, useTheme } from "@/context/ThemeContext";
import ErrorBoundary from "@/components/ErrorBoundary";
import { AdminOnly, Booting, Guard, PublicOnly } from "@/components/Guards";
import Landing from "@/pages/Landing";
import Login from "@/pages/Login";
import Register from "@/pages/Register";
import Hop from "@/pages/Hop";
import NotFound from "@/pages/NotFound";

// The console pulls in recharts; keep it out of the landing bundle.
const Shell = lazy(() => import("@/components/Shell"));
const Dashboard = lazy(() => import("@/pages/Dashboard"));
const Links = lazy(() => import("@/pages/Links"));
const LinkDetail = lazy(() => import("@/pages/LinkDetail"));
const Bench = lazy(() => import("@/pages/Bench"));
const Ops = lazy(() => import("@/pages/Ops"));

const toastStyle = {
  background: "rgb(var(--kx-surface))",
  color: "rgb(var(--kx-ink))",
  border: "1px solid rgb(var(--kx-line))",
  borderRadius: "8px",
  fontSize: "13px",
};

// sonner needs to know the theme for its icons; read it from ThemeContext.
function ThemedToaster() {
  const { theme } = useTheme();
  return <Toaster theme={theme} position="bottom-right" toastOptions={{ style: toastStyle }} />;
}

export function AppRoutes() {
  return (
    <Suspense fallback={<Booting label="Loading…" />}>
      <Routes>
        <Route path="/" element={<Landing />} />
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
          <Route
            path="bench"
            element={
              <AdminOnly>
                <Bench />
              </AdminOnly>
            }
          />
          <Route path="ops" element={<Ops />} />
          <Route path="*" element={<NotFound embedded />} />
        </Route>
        <Route path="/:code" element={<Hop />} />
        <Route path="*" element={<NotFound />} />
      </Routes>
    </Suspense>
  );
}

export default function App() {
  return (
    <ThemeProvider>
      <ErrorBoundary>
        <BrowserRouter>
          <AuthProvider>
            <ThemedToaster />
            <AppRoutes />
          </AuthProvider>
        </BrowserRouter>
      </ErrorBoundary>
    </ThemeProvider>
  );
}
