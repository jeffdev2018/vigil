/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { ListAutopilotsResponse } from "../types";
import { useDeleteAutopilot } from "./mutations";
import { autopilotKeys } from "./queries";

vi.mock("../hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

function createWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

// JEF-397: a delete awaits the server. The row leaves the list only after a
// confirmed success — a refusal must leave the cache untouched.
describe("useDeleteAutopilot", () => {
  let qc: QueryClient;
  const list = {
    autopilots: [{ id: "a1" }, { id: "a2" }],
    total: 2,
  } as unknown as ListAutopilotsResponse;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    qc.setQueryData(autopilotKeys.list("ws-1"), list);
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("keeps the autopilot in the list when the server refuses", async () => {
    setApiInstance({
      deleteAutopilot: vi.fn().mockRejectedValue(new Error("403")),
    } as unknown as ApiClient);
    const { result } = renderHook(() => useDeleteAutopilot(), { wrapper: createWrapper(qc) });

    await act(async () => {
      await result.current.mutateAsync("a1").catch(() => undefined);
    });

    expect(qc.getQueryData<ListAutopilotsResponse>(autopilotKeys.list("ws-1"))?.total).toBe(2);
  });

  it("drops the autopilot once the server confirms", async () => {
    setApiInstance({ deleteAutopilot: vi.fn().mockResolvedValue(undefined) } as unknown as ApiClient);
    const { result } = renderHook(() => useDeleteAutopilot(), { wrapper: createWrapper(qc) });

    await act(async () => {
      await result.current.mutateAsync("a1");
    });

    const after = qc.getQueryData<ListAutopilotsResponse>(autopilotKeys.list("ws-1"));
    expect(after?.autopilots.map((a) => a.id)).toEqual(["a2"]);
    expect(after?.total).toBe(1);
  });
});
