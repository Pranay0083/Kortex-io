import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { api, refreshSession, setSessionExpiredHandler } from "@/lib/api";

const AuthCtx = createContext(null);

// /auth/me returns the user object; tolerate a { user } envelope too.
const unwrapUser = (data) =>
  data && typeof data === "object" && "user" in data ? data.user : data;

export function AuthProvider({ children }) {
  const [user, setUser] = useState(null); // null = unknown, false = logged out
  const [ready, setReady] = useState(false);
  const userRef = useRef(null);
  const navigate = useNavigate();

  const applyUser = useCallback((u) => {
    userRef.current = u || false;
    setUser(u || false);
  }, []);

  // Boot: always ask the server — the session lives in httpOnly cookies we can't see.
  useEffect(() => {
    let alive = true;
    (async () => {
      let u = false;
      try {
        const { data } = await api.get("/auth/me", { skipAuthRefresh: true });
        u = unwrapUser(data) || false;
      } catch (e) {
        // Access cookie may simply have expired: try the refresh cookie once, silently.
        if (e?.response?.status === 401) {
          try {
            u = (await refreshSession()) || false;
          } catch {
            u = false;
          }
        }
      }
      if (!alive) return;
      applyUser(u);
      setReady(true);
    })();
    return () => {
      alive = false;
    };
  }, [applyUser]);

  // A request 401'd and the refresh failed: drop the session (once) and go to /login.
  useEffect(() => {
    setSessionExpiredHandler(() => {
      if (!userRef.current) return;
      applyUser(false);
      toast.error("Session expired — please sign in again");
      navigate("/login", { replace: true });
    });
    return () => setSessionExpiredHandler(null);
  }, [applyUser, navigate]);

  const authenticate = useCallback(
    async (path, payload) => {
      const { data } = await api.post(`/auth/${path}`, payload);
      const u = unwrapUser(data);
      applyUser(u);
      return u;
    },
    [applyUser],
  );

  const login = useCallback(
    (email, password) => authenticate("login", { email, password }),
    [authenticate],
  );
  // Shared trial account; only available while the server runs in demo mode.
  const demoLogin = useCallback(() => authenticate("demo", null), [authenticate]);
  const register = useCallback(
    (email, password, name) => authenticate("register", { email, password, name }),
    [authenticate],
  );

  const logout = useCallback(async () => {
    try {
      await api.post("/auth/logout", null, { skipAuthRefresh: true });
    } catch {
      /* cookies already gone server-side */
    }
    applyUser(false);
  }, [applyUser]);

  const value = useMemo(
    () => ({ user, ready, login, demoLogin, register, logout }),
    [user, ready, login, demoLogin, register, logout],
  );

  return <AuthCtx.Provider value={value}>{children}</AuthCtx.Provider>;
}

export const useAuth = () => useContext(AuthCtx);
