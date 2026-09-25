/**
 * @vitest-environment jsdom
 */
import { describe, expect, it, vi, afterEach } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { AgentRuntime } from "../types";
import { useRuntimeHealth } from "./use-runtime-health";

function makeRuntime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "rt-1",
    workspace_id: "ws-1",
    daemon_id: "daemon-1",
    name: "Test Runtime",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "",
    metadata: {},
    owner_id: null,
    visibility: "private",
    last_seen_at: new Date().toISOString(),
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
    ...overrides,
  };
}

function createWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

describe("useRuntimeHealth", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    setApiInstance(undefined as unknown as ApiClient);
  });

  it("returns offline, not a permanent loading state, when the runtime list is loaded but the id isn't in it", async () => {
    const listRuntimes = vi.fn().mockResolvedValue([makeRuntime({ id: "some-other-runtime" })]);
    setApiInstance({ listRuntimes } as unknown as ApiClient);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });

    const { result } = renderHook(
      () => useRuntimeHealth("ws-1", "deleted-runtime"),
      { wrapper: createWrapper(qc) },
    );

    await act(async () => {
      await vi.waitFor(() => expect(listRuntimes).toHaveBeenCalled());
    });
    await act(async () => {
      await vi.waitFor(() => expect(result.current).not.toBe("loading"));
    });

    expect(result.current).toBe("offline");
  });
});
