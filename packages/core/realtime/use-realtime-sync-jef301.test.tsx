/**
 * @vitest-environment jsdom
 *
 * JEF-301: refreshMap coverage for the event families that previously had no
 * client-side WS consumer — contest, project_resource, goal, issue_view, and
 * decision. Each assertion fires a synthetic frame through the same onAny
 * dispatcher useRealtimeSync wires up in production and checks the exact
 * queryKey(s) invalidated, the same harness style as
 * use-realtime-sync-ws-instance.test.tsx's "daemon changes liveness" case.
 */
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import type { WSClient } from "../api/ws-client";
import { contestKeys } from "../issues/contest";
import { issueViewKeys } from "../issue-views/queries";
import { goalKeys } from "../issues/goal-loop";
import { issueKeys } from "../issues/queries";
import { useRealtimeSync, type RealtimeSyncStores } from "./use-realtime-sync";

vi.mock("../platform/workspace-storage", () => ({
  getCurrentWsId: () => "ws-1",
  getCurrentSlug: () => "test-ws",
  createWorkspaceAwareStorage: (adapter: unknown) => adapter,
  registerForWorkspaceRehydration: () => {},
}));

vi.mock("../paths", () => ({
  useHasOnboarded: () => true,
  resolvePostAuthDestination: () => "/",
}));

function createMockWs(): WSClient {
  return {
    on: vi.fn(() => () => {}),
    onAny: vi.fn(() => () => {}),
    onReconnect: vi.fn(() => () => {}),
  } as unknown as WSClient;
}

function createStores(): RealtimeSyncStores {
  return {
    authStore: Object.assign(() => ({}), {
      getState: () => ({ user: { id: "u1" } }),
      subscribe: () => () => {},
      setState: () => {},
      destroy: () => {},
    }),
  } as unknown as RealtimeSyncStores;
}

function createWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

describe("useRealtimeSync — JEF-301 new event families", () => {
  let qc: QueryClient;
  let invalidateSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    invalidateSpy = vi.spyOn(qc, "invalidateQueries");
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  function fireAndFlush(onAny: (msg: never) => void, type: string) {
    invalidateSpy.mockClear();
    onAny({ type, payload: {} } as never);
    vi.advanceTimersByTime(100);
  }

  it("invalidates the contest prefix on contest:updated", () => {
    const ws = createMockWs();
    renderHook(() => useRealtimeSync(ws, createStores()), {
      wrapper: createWrapper(qc),
    });
    const onAny = vi.mocked(ws.onAny).mock.calls[0]?.[0];
    expect(onAny).toBeDefined();

    fireAndFlush(onAny!, "contest:updated");

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: contestKeys.all("ws-1") });
  });

  it("invalidates every project's resources list on project_resource:*", () => {
    const ws = createMockWs();
    renderHook(() => useRealtimeSync(ws, createStores()), {
      wrapper: createWrapper(qc),
    });
    const onAny = vi.mocked(ws.onAny).mock.calls[0]?.[0];
    expect(onAny).toBeDefined();

    // Seed two projects' resource lists plus an unrelated project query —
    // the predicate must hit only the "resources" suffix, for any project.
    qc.setQueryData(["projects", "ws-1", "detail", "proj-a", "resources"], []);
    qc.setQueryData(["projects", "ws-1", "detail", "proj-b", "resources"], []);
    qc.setQueryData(["projects", "ws-1", "detail", "proj-a"], {});
    qc.setQueryData(["projects", "ws-1", "list"], []);

    fireAndFlush(onAny!, "project_resource:created");

    const calls = invalidateSpy.mock.calls;
    expect(calls).toHaveLength(1);
    const predicate = (calls[0]?.[0] as { predicate?: (q: { queryKey: unknown[] }) => boolean })?.predicate;
    expect(predicate).toBeDefined();
    expect(predicate!({ queryKey: ["projects", "ws-1", "detail", "proj-a", "resources"] })).toBe(true);
    expect(predicate!({ queryKey: ["projects", "ws-1", "detail", "proj-b", "resources"] })).toBe(true);
    expect(predicate!({ queryKey: ["projects", "ws-1", "detail", "proj-a"] })).toBe(false);
    expect(predicate!({ queryKey: ["projects", "ws-1", "list"] })).toBe(false);
    // A resources list under a different workspace must not match.
    expect(predicate!({ queryKey: ["projects", "ws-2", "detail", "proj-a", "resources"] })).toBe(false);
  });

  it("invalidates the goal prefix on goal:updated", () => {
    const ws = createMockWs();
    renderHook(() => useRealtimeSync(ws, createStores()), {
      wrapper: createWrapper(qc),
    });
    const onAny = vi.mocked(ws.onAny).mock.calls[0]?.[0];
    expect(onAny).toBeDefined();

    fireAndFlush(onAny!, "goal:updated");

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: goalKeys.all("ws-1") });
  });

  it("invalidates the issue-views prefix on issue_view:deleted", () => {
    const ws = createMockWs();
    renderHook(() => useRealtimeSync(ws, createStores()), {
      wrapper: createWrapper(qc),
    });
    const onAny = vi.mocked(ws.onAny).mock.calls[0]?.[0];
    expect(onAny).toBeDefined();

    fireAndFlush(onAny!, "issue_view:deleted");

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: issueViewKeys.all("ws-1") });
  });

  it("invalidates both project decisions and issue decisions on decision:created", () => {
    const ws = createMockWs();
    renderHook(() => useRealtimeSync(ws, createStores()), {
      wrapper: createWrapper(qc),
    });
    const onAny = vi.mocked(ws.onAny).mock.calls[0]?.[0];
    expect(onAny).toBeDefined();

    fireAndFlush(onAny!, "decision:created");

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ["decisions", "ws-1"] });
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: issueKeys.decisionsAll("ws-1") });
  });
});
