import { describe, expect, it, vi } from "vitest";
import type { QueryClient } from "@tanstack/react-query";
import type { ApiClient } from "../api/client";
import { ApiError } from "../api/client";
import type { StorageAdapter, User, Workspace } from "../types";
import { createAuthStore } from "./store";

const fakeUser: User = {
  id: "u1",
  name: "Alice",
  email: "alice@example.com",
  avatar_url: null,
} as User;

function makeStorage(initial: Record<string, string> = {}): StorageAdapter & {
  snapshot: () => Record<string, string>;
} {
  const data = { ...initial };
  return {
    getItem: (k) => data[k] ?? null,
    setItem: (k, v) => {
      data[k] = v;
    },
    removeItem: (k) => {
      delete data[k];
    },
    snapshot: () => ({ ...data }),
  };
}

function makeApi(getMe: () => Promise<User>): ApiClient {
  return {
    setToken: vi.fn(),
    getMe,
    // Only the methods touched by store.initialize are needed. Cast to
    // ApiClient for type compatibility — the store treats it opaquely.
  } as unknown as ApiClient;
}

describe("authStore.initialize — token mode", () => {
  it("keeps the stored token when getMe fails with a non-401 ApiError (e.g. 500)", async () => {
    const storage = makeStorage({ multica_token: "t" });
    const api = makeApi(() =>
      Promise.reject(new ApiError("server error", 500, "Internal Server Error")),
    );
    const store = createAuthStore({ api, storage });

    await store.getState().initialize();

    expect(store.getState().user).toBeNull();
    expect(store.getState().isLoading).toBe(false);
    expect(storage.snapshot().multica_token).toBe("t");
  });

  it("keeps the stored token on a network failure (non-ApiError throw)", async () => {
    const storage = makeStorage({ multica_token: "t" });
    const api = makeApi(() => Promise.reject(new TypeError("fetch failed")));
    const store = createAuthStore({ api, storage });

    await store.getState().initialize();

    expect(store.getState().user).toBeNull();
    expect(storage.snapshot().multica_token).toBe("t");
  });

  it("on 401, leaves storage cleanup to ApiClient.onUnauthorized and resets state", async () => {
    // Simulate the real path: ApiClient fires onUnauthorized on 401, which
    // removes the token from storage. The store's catch block must not
    // duplicate or short-circuit this — it should only reset in-memory
    // auth state.
    const storage = makeStorage({ multica_token: "t" });
    const api = makeApi(() => {
      storage.removeItem("multica_token"); // stand-in for onUnauthorized
      return Promise.reject(new ApiError("unauthorized", 401, "Unauthorized"));
    });
    const store = createAuthStore({ api, storage });

    await store.getState().initialize();

    expect(store.getState().user).toBeNull();
    expect(storage.snapshot().multica_token).toBeUndefined();
  });

  it("populates user when getMe succeeds", async () => {
    const storage = makeStorage({ multica_token: "t" });
    const api = makeApi(() => Promise.resolve(fakeUser));
    const store = createAuthStore({ api, storage });

    await store.getState().initialize();

    expect(store.getState().user).toEqual(fakeUser);
    expect(storage.snapshot().multica_token).toBe("t");
  });
});

// ---------------------------------------------------------------------------
// smartGateLogin — workspace cache prefill ordering (race regression guard)
//
// The /login page's outer redirect effect peeks at the workspace list query
// cache synchronously as soon as `user` becomes non-null. If the cache is
// empty at that instant, a plain user (no cli_callback / next) gets routed
// to /workspaces/new instead of their first workspace. To close the race,
// the store seeds the query cache *before* calling `set({ user })` — these
// tests nail that ordering down.
// ---------------------------------------------------------------------------

const fakeWs: Workspace = {
  id: "ws-1",
  name: "Alpha",
  slug: "alpha",
} as unknown as Workspace;

describe("authStore.smartGateLogin — workspace cache prefill", () => {
  it("seeds workspace list cache before set({ user }) even with a slow listWorkspaces", async () => {
    // Build a fake api whose listWorkspaces resolves asynchronously (on
    // the next microtask) so any mistaken ordering has a clear window.
    let workspacesResolved = false;
    const api: ApiClient = {
      setToken: vi.fn(),
      smartGateLogin: vi.fn().mockResolvedValue({
        token: "jwt",
        user: fakeUser,
      }),
      listWorkspaces: vi.fn(async () => {
        // Yield once — the real network yields many more, but the
        // ordering hazard is identical after the first yield.
        await Promise.resolve();
        workspacesResolved = true;
        return [fakeWs];
      }),
    } as unknown as ApiClient;

    const setQueryData = vi.fn();
    const qc = { setQueryData } as unknown as QueryClient;
    const storage = makeStorage();

    const store = createAuthStore({
      api,
      storage,
      getQueryClient: () => qc,
    });

    // Track the exact moment the subscriber sees `user` flip to non-null.
    // If the cache hasn't been seeded by then, the race is alive.
    let cacheWhenUserSet: unknown = "UNSET";
    const unsub = store.subscribe((state, prev) => {
      if (!prev.user && state.user) {
        // Read what setQueryData saw at the moment user transitions.
        cacheWhenUserSet =
          setQueryData.mock.calls.length > 0
            ? setQueryData.mock.calls[0]?.[1]
            : "EMPTY";
      }
    });

    await store.getState().smartGateLogin();

    unsub();

    // Cache must already be seeded by the time `user` is set.
    expect(cacheWhenUserSet).toEqual([fakeWs]);
    expect(workspacesResolved).toBe(true);
    // And the exact setQueryData call shape matches workspaceKeys.list().
    expect(setQueryData).toHaveBeenCalledWith(
      ["workspaces", "list"],
      [fakeWs],
    );
    // Strict ordering via invocationCallOrder — setQueryData must come
    // before the zustand set that notifies subscribers. We can infer
    // the set order because smartGateLogin's resolution implies
    // set({ user }) has run, and our subscriber captured the cache
    // contents at that transition.
    expect(setQueryData.mock.invocationCallOrder[0]).toBeLessThan(
      // smartGateLogin was called before setQueryData (it's the outer
      // call), so this is a sanity check that ordering numbers are
      // comparable, not a meaningful ordering assertion.
      (api.listWorkspaces as ReturnType<typeof vi.fn>).mock
        .invocationCallOrder[0]! + 1_000_000,
    );
  });

  it("continues login when listWorkspaces fails (cache miss is non-fatal)", async () => {
    const api: ApiClient = {
      setToken: vi.fn(),
      smartGateLogin: vi.fn().mockResolvedValue({
        token: "jwt",
        user: fakeUser,
      }),
      listWorkspaces: vi
        .fn()
        .mockRejectedValue(new ApiError("boom", 500, "Server Error")),
    } as unknown as ApiClient;

    const setQueryData = vi.fn();
    const qc = { setQueryData } as unknown as QueryClient;

    const store = createAuthStore({
      api,
      storage: makeStorage(),
      getQueryClient: () => qc,
    });

    // Suppress the expected warning so it doesn't pollute test output.
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});

    const user = await store.getState().smartGateLogin();

    expect(user).toEqual(fakeUser);
    // Login still succeeded.
    expect(store.getState().user).toEqual(fakeUser);
    // No cache write happened because listWorkspaces failed — the outer
    // page will fall back to fetching the list itself.
    expect(setQueryData).not.toHaveBeenCalled();
    warnSpy.mockRestore();
  });

  it("skips cache prefill when no QueryClient is registered", async () => {
    const api: ApiClient = {
      setToken: vi.fn(),
      smartGateLogin: vi.fn().mockResolvedValue({
        token: "jwt",
        user: fakeUser,
      }),
      listWorkspaces: vi.fn(),
    } as unknown as ApiClient;

    const store = createAuthStore({
      api,
      storage: makeStorage(),
      // getQueryClient not provided — simulates early boot / tests
      // that don't mount the QueryProvider.
    });

    const user = await store.getState().smartGateLogin();
    expect(user).toEqual(fakeUser);
    // Must not have reached out for the workspace list — no QueryClient
    // to write it to.
    expect(api.listWorkspaces).not.toHaveBeenCalled();
  });
});
