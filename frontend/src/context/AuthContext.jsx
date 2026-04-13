import { createContext, useContext, useEffect, useState, useCallback } from "react";
import { api } from "@/lib/api";

const AuthCtx = createContext(null);

export function AuthProvider({ children }) {
  const [user, setUser] = useState(null);
  const [ready, setReady] = useState(false);

  const load = useCallback(async () => {
    const t = localStorage.getItem("kortex_token");
    if (!t) {
      setUser(false);
      setReady(true);
      return;
    }
    try {
      const { data } = await api.get("/auth/me");
      setUser(data);
    } catch {
      localStorage.removeItem("kortex_token");
      setUser(false);
    }
    setReady(true);
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const authenticate = async (path, payload) => {
    const { data } = await api.post(`/auth/${path}`, payload);
    localStorage.setItem("kortex_token", data.access_token);
    setUser(data.user);
    return data.user;
  };

  const login = (email, password) => authenticate("login", { email, password });
  const register = (email, password, name) => authenticate("register", { email, password, name });

  const logout = async () => {
    try {
      await api.post("/auth/logout");
    } catch {
      /* token already gone */
    }
    localStorage.removeItem("kortex_token");
    setUser(false);
  };

  return (
    <AuthCtx.Provider value={{ user, ready, login, register, logout }}>{children}</AuthCtx.Provider>
  );
}

export const useAuth = () => useContext(AuthCtx);
