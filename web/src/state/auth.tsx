import {
  createContext, useContext, useEffect, useMemo, useState, type ReactNode,
} from "react";
import { api, setToken as setApiToken, ApiError } from "../api/client";
import { wsClient } from "../api/ws";
import type { User } from "../api/types";

interface AuthState {
  user: User | null;
  token: string | null;
  ready: boolean; // initial session restore finished
  login: (email: string, password: string) => Promise<void>;
  register: (email: string, password: string) => Promise<void>;
  logout: () => void;
  refresh: () => Promise<void>;
}

const Ctx = createContext<AuthState | null>(null);
const STORAGE_KEY = "cryptoex.token";

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [token, setTok] = useState<string | null>(null);
  const [ready, setReady] = useState(false);

  // Apply a token everywhere it is needed (REST header + WS auth).
  function applyToken(t: string | null, u: User | null) {
    setTok(t);
    setUser(u);
    setApiToken(t);
    wsClient.setAuth(t, u?.id ?? null);
    if (t) localStorage.setItem(STORAGE_KEY, t);
    else localStorage.removeItem(STORAGE_KEY);
  }

  // Restore a saved session on first load.
  useEffect(() => {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (!saved) {
      setReady(true);
      return;
    }
    setApiToken(saved);
    api
      .me()
      .then((u) => applyToken(saved, u))
      .catch((e) => {
        if (e instanceof ApiError && e.status === 401) applyToken(null, null);
      })
      .finally(() => setReady(true));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const value = useMemo<AuthState>(
    () => ({
      user,
      token,
      ready,
      login: async (email, password) => {
        const r = await api.login(email, password);
        applyToken(r.token, r.user);
      },
      register: async (email, password) => {
        const r = await api.register(email, password);
        applyToken(r.token, r.user);
      },
      logout: () => applyToken(null, null),
      refresh: async () => {
        const u = await api.me();
        setUser(u);
      },
    }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [user, token, ready],
  );

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useAuth(): AuthState {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}
