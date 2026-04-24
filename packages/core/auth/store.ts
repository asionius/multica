import { create } from "zustand";
import type { QueryClient } from "@tanstack/react-query";
import type { User, StorageAdapter } from "../types";
import { identify as identifyAnalytics, resetAnalytics } from "../analytics";
import { ApiError, type ApiClient } from "../api/client";
import { setCurrentWorkspace } from "../platform/workspace-storage";
import { workspaceKeys } from "../workspace/queries";

export interface AuthStoreOptions {
  api: ApiClient;
  storage: StorageAdapter;
  onLogin?: () => void;
  onLogout?: () => void;
  /** When true, rely on HttpOnly cookies instead of localStorage for auth tokens. */
  cookieAuth?: boolean;
  /**
   * Late-bound accessor to the React Query client. Auth actions use this to
   * seed the workspace list cache *before* setting `user`, so any subscriber
   * that reacts to the user transition (e.g. the /login page's redirect
   * effect) observes a populated cache and routes to the first workspace
   * instead of /workspaces/new. Returns null before the QueryProvider
   * registers the client, in which case the cache prefill is skipped (the
   * outer page will fall back to fetching the list itself).
   */
  getQueryClient?: () => QueryClient | null;
}

export interface AuthState {
  user: User | null;
  isLoading: boolean;

  initialize: () => Promise<void>;
  sendCode: (email: string) => Promise<void>;
  verifyCode: (email: string, code: string) => Promise<User>;
  smartGateLogin: () => Promise<User>;
  loginWithGoogle: (code: string, redirectUri: string) => Promise<User>;
  loginWithToken: (token: string) => Promise<User>;
  logout: () => void;
  setUser: (user: User) => void;
  refreshMe: () => Promise<void>;
}

export function createAuthStore(options: AuthStoreOptions) {
  const { api, storage, onLogin, onLogout, cookieAuth, getQueryClient } = options;

  // Fetch the workspace list and seed the React Query cache. Runs before
  // `set({ user })` on every login path so subscribers to the user
  // transition see a populated cache and can route correctly. Failures are
  // swallowed — the outer page will fall back to fetching the list itself,
  // and we never want a workspace-list error to block login itself.
  const prefillWorkspaceCache = async () => {
    const qc = getQueryClient?.();
    if (!qc) return;
    try {
      const wsList = await api.listWorkspaces();
      qc.setQueryData(workspaceKeys.list(), wsList);
    } catch (err) {
      // Non-fatal: leave the cache untouched; outer page recovers.
      console.warn("auth: failed to prefill workspace list cache", err);
    }
  };

  return create<AuthState>((set) => ({
    user: null,
    isLoading: true,

    initialize: async () => {
      if (cookieAuth) {
        // In cookie mode, the HttpOnly cookie is sent automatically.
        // Try to fetch the current user — if the cookie exists the server will accept it.
        try {
          const user = await api.getMe();
          set({ user, isLoading: false });
        } catch {
          set({ user: null, isLoading: false });
        }
        return;
      }

      // Token mode: read from localStorage (Electron / legacy).
      const token = storage.getItem("multica_token");
      if (!token) {
        set({ isLoading: false });
        return;
      }

      api.setToken(token);

      try {
        const user = await api.getMe();
        set({ user, isLoading: false });
      } catch (err) {
        // Only clear the stored token on a genuine auth failure (401). For
        // transient errors — network blips, backend rolling restarts, 5xx,
        // aborted fetches — keep the token so the next initialize() (next
        // page load or focus-refresh) can retry. The 401 path's token
        // cleanup is handled upstream by ApiClient.handleUnauthorized via
        // the onUnauthorized callback; we only need to reset the in-memory
        // user + workspace state here.
        if (err instanceof ApiError && err.status === 401) {
          setCurrentWorkspace(null, null);
        }
        set({ user: null, isLoading: false });
      }
    },

    sendCode: async (email: string) => {
      await api.sendCode(email);
    },

    verifyCode: async (email: string, code: string) => {
      const { token, user } = await api.verifyCode(email, code);
      if (!cookieAuth) {
        // Token mode: persist for Electron / legacy.
        storage.setItem("multica_token", token);
        api.setToken(token);
      }
      // Prefill workspace cache *before* notifying user subscribers.
      // See prefillWorkspaceCache docstring for why.
      await prefillWorkspaceCache();
      onLogin?.();
      identifyAnalytics(user.id, { email: user.email, name: user.name });
      set({ user });
      return user;
    },

    smartGateLogin: async () => {
      const { token, user } = await api.smartGateLogin();
      if (!cookieAuth) {
        storage.setItem("multica_token", token);
        api.setToken(token);
      }
      await prefillWorkspaceCache();
      onLogin?.();
      set({ user });
      return user;
    },

    loginWithGoogle: async (code: string, redirectUri: string) => {
      const { token, user } = await api.googleLogin(code, redirectUri);
      if (!cookieAuth) {
        storage.setItem("multica_token", token);
        api.setToken(token);
      }
      await prefillWorkspaceCache();
      onLogin?.();
      identifyAnalytics(user.id, { email: user.email, name: user.name });
      set({ user });
      return user;
    },

    loginWithToken: async (token: string) => {
      storage.setItem("multica_token", token);
      api.setToken(token);
      const user = await api.getMe();
      await prefillWorkspaceCache();
      onLogin?.();
      identifyAnalytics(user.id, { email: user.email, name: user.name });
      set({ user, isLoading: false });
      return user;
    },

    logout: () => {
      if (cookieAuth) {
        // Clear server-side HttpOnly cookie.
        api.logout().catch(() => {});
      }
      storage.removeItem("multica_token");
      api.setToken(null);
      setCurrentWorkspace(null, null);
      resetAnalytics();
      onLogout?.();
      set({ user: null });
    },

    setUser: (user: User) => {
      set({ user });
    },

    refreshMe: async () => {
      const user = await api.getMe();
      set({ user });
    },
  }));
}
