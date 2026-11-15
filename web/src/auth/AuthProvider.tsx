import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";

import { api, type Me } from "../api/client";
import { useHost } from "../host/HostContext";
import type { HostBridge } from "../host/types";
import { setSessionRefresher, setSessionToken, setUnauthorizedHandler } from "./session";
import { signInWithTelegram } from "./telegram";
import { LoginRequiredScreen, LinkRequiredScreen, SessionErrorScreen, SignInProgress } from "./screens";

/** Result of loading the browser profile. */
export type MeResult =
  | { kind: "ok"; user: Me }
  | { kind: "unauthorized" }
  | { kind: "error"; message: string };

/** Collaborators of the auth layer. Tests pass a fake. */
export interface AuthDeps {
  loadMe: () => Promise<MeResult>;
  signInTelegram: (initData: string) => Promise<Awaited<ReturnType<typeof signInWithTelegram>>>;
  logout: () => Promise<void>;
}

/** Real collaborators: the HTTP API through the cookie or the in-memory token. */
export const defaultAuthDeps: AuthDeps = {
  async loadMe() {
    const { data, response, error } = await api.GET("/me");
    if (data) {
      return { kind: "ok", user: data };
    }
    if (response.status === 401) {
      return { kind: "unauthorized" };
    }
    return { kind: "error", message: error?.message ?? `Сервер ответил ошибкой ${response.status}` };
  },
  signInTelegram(initData) {
    return signInWithTelegram({ initData });
  },
  async logout() {
    await api.POST("/auth/logout", { headers: { "Content-Type": "application/json" } });
  },
};

/** What the authenticated part of the app sees. */
export interface AuthContextValue {
  user: Me;
  host: HostBridge;
  /** Replaces the cached profile after `PATCH /me`. */
  updateUser: (user: Me) => void;
  /** Revokes the session and returns to the sign-in screen. */
  signOut: () => Promise<void>;
}

type AuthState =
  | { kind: "checking" }
  | { kind: "ready"; user: Me }
  | { kind: "login-required" }
  | { kind: "link-required"; linkUrl: string }
  | { kind: "error"; message: string };

const AuthContext = createContext<AuthContextValue | null>(null);

/** Access to the signed-in user. Renders only under `AuthProvider`. */
export function useAuth(): AuthContextValue {
  const value = useContext(AuthContext);
  if (!value) {
    throw new Error("useAuth must be used inside AuthProvider");
  }
  return value;
}

/** Props of the auth gate. */
export interface AuthProviderProps {
  children: ReactNode;
  /** Injectable collaborators; defaults to the real API. */
  deps?: AuthDeps;
}

/**
 * Authenticates the page and renders the shell only for a signed-in user.
 *
 * * Telegram Mini App — exchanges `initData` for a Bearer token kept in memory.
 * * Browser (and the Matrix stub) — relies on the `HttpOnly` cookie session.
 *
 * A missing browser session shows the «Нужно войти» screen; an unbound Telegram
 * account shows the binding link from `link_required`.
 */
export function AuthProvider({ children, deps = defaultAuthDeps }: AuthProviderProps) {
  const host = useHost();
  const [state, setState] = useState<AuthState>({ kind: "checking" });
  const depsRef = useRef(deps);
  depsRef.current = deps;
  // A binding flow must keep its screen even if the 401 handler fires right
  // after the refresh: there is no session to restore until the user binds.
  const linkRequiredRef = useRef(false);

  const applyTelegramAuth = useCallback(
    async (initData: string): Promise<string | null> => {
      const result = await depsRef.current.signInTelegram(initData);
      if (result.status === "ok" && result.token) {
        linkRequiredRef.current = false;
        setSessionToken(result.token);
        return result.token;
      }
      if (result.status === "link_required" && result.link_url) {
        linkRequiredRef.current = true;
        setState({ kind: "link-required", linkUrl: result.link_url });
      }
      return null;
    },
    [],
  );

  useEffect(() => {
    host.init();
  }, [host]);

  useEffect(() => {
    let cancelled = false;

    setUnauthorizedHandler(() => {
      if (cancelled || linkRequiredRef.current) {
        return;
      }
      if (host.isTelegram) {
        setState({ kind: "error", message: "Не удалось восстановить сессию Telegram. Попробуйте открыть приложение заново." });
      } else {
        setState({ kind: "login-required" });
      }
    });

    if (host.isTelegram) {
      setSessionRefresher(async () => {
        try {
          return await applyTelegramAuth(host.initData);
        } catch {
          return null;
        }
      });

      applyTelegramAuth(host.initData)
        .then(async (token) => {
          if (cancelled) {
            return;
          }
          if (token) {
            const me = await depsRef.current.loadMe();
            if (cancelled) {
              return;
            }
            if (me.kind === "ok") {
              setState({ kind: "ready", user: me.user });
              return;
            }
            if (me.kind === "unauthorized") {
              setState({ kind: "error", message: "Сессия Telegram не принята сервером. Попробуйте ещё раз." });
              return;
            }
            setState({ kind: "error", message: me.message });
          }
        })
        .catch((cause: unknown) => {
          if (!cancelled) {
            setState({ kind: "error", message: errorMessage(cause) });
          }
        });
    } else {
      depsRef.current
        .loadMe()
        .then((me) => {
          if (cancelled) {
            return;
          }
          switch (me.kind) {
            case "ok":
              setState({ kind: "ready", user: me.user });
              break;
            case "unauthorized":
              setState({ kind: "login-required" });
              break;
            case "error":
              setState({ kind: "error", message: me.message });
              break;
          }
        })
        .catch((cause: unknown) => {
          if (!cancelled) {
            setState({ kind: "error", message: errorMessage(cause) });
          }
        });
    }

    return () => {
      cancelled = true;
      setSessionRefresher(null);
      setUnauthorizedHandler(null);
    };
  }, [host, applyTelegramAuth]);

  const updateUser = useCallback((user: Me) => {
    setState((current) => (current.kind === "ready" ? { kind: "ready", user } : current));
  }, []);

  const signOut = useCallback(async () => {
    await depsRef.current.logout();
    setSessionToken(null);
    if (host.isTelegram) {
      setState({ kind: "error", message: "Сессия закрыта. Откройте приложение заново из Telegram." });
      return;
    }
    setState({ kind: "login-required" });
  }, [host]);

  switch (state.kind) {
    case "checking":
      return <SignInProgress />;
    case "login-required":
      return <LoginRequiredScreen />;
    case "link-required":
      return (
        <LinkRequiredScreen
          linkUrl={state.linkUrl}
          onOpen={(url) => host.openLink(url)}
        />
      );
    case "error":
      return <SessionErrorScreen message={state.message} onRetry={() => window.location.reload()} />;
    case "ready":
      return (
        <AuthContext.Provider
          value={{ user: state.user, host, updateUser, signOut }}
        >
          {children}
        </AuthContext.Provider>
      );
  }
}

function errorMessage(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause);
}
