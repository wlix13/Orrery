// Auth/credentials state: token + API base from localStorage, and the
// resulting OrreryClient (real fetch client, or the in-memory mock when the
// token is literally "mock" or VITE_MOCK=1). Pages consume this via
// useAuth() instead of importing api/client or api/mock directly.

import {
  createContext,
  createElement,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import type { Me } from "../api/types";
import {
  clearStoredCredentials,
  getStoredApiBase,
  getStoredToken,
  httpClient,
  isMockToken,
  setStoredCredentials,
  type OrreryClient,
} from "../api/client";
import { mockClient } from "../api/mock";

interface AuthState {
  token: string;
  apiBase: string;
  client: OrreryClient;
  isMock: boolean;
  /** The collector rejected our credentials, so show the token gate. */
  needsToken: boolean;
  /** True until the first /api/me probe answers. */
  checking: boolean;
  /** Who the collector says we are; null until the probe succeeds. */
  me: Me | null;
  setCredentials: (token: string, apiBase: string) => void;
  signOut: () => void;
}

const AuthContext = createContext<AuthState | null>(null);

const forcedMock = import.meta.env.VITE_MOCK === "1";

export function AuthProvider({ children }: { children: ReactNode }) {
  const [token, setToken] = useState(getStoredToken);
  const [apiBase, setApiBase] = useState(getStoredApiBase);

  const setCredentials = useCallback((newToken: string, newApiBase: string) => {
    setStoredCredentials(newToken, newApiBase);
    setToken(newToken);
    setApiBase(newApiBase);
  }, []);

  const signOut = useCallback(() => {
    clearStoredCredentials();
    setToken("");
    setApiBase("");
  }, []);

  const isMock = forcedMock || isMockToken(token);
  const client = useMemo<OrreryClient>(() => (isMock ? mockClient : httpClient), [isMock]);

  const [me, setMe] = useState<Me | null>(null);
  const [checking, setChecking] = useState(true);

  // The server decides whether we are authenticated, not the presence of a
  // stored token: behind Cloudflare Access the browser holds a cookie and
  // there is no token to store.
  useEffect(() => {
    let cancelled = false;

    setChecking(true);
    client
      .getMe()
      .then((who) => {
        if (!cancelled) {
          setMe(who);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setMe(null);
        }
      })
      .finally(() => {
        if (!cancelled) {
          setChecking(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [client, token, apiBase]);

  const needsToken = !checking && me === null;

  const value = useMemo<AuthState>(
    () => ({ token, apiBase, client, isMock, needsToken, checking, me, setCredentials, signOut }),
    [token, apiBase, client, isMock, needsToken, checking, me, setCredentials, signOut],
  );

  return createElement(AuthContext.Provider, { value }, children);
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within an AuthProvider");
  return ctx;
}
