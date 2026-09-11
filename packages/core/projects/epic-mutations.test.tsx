/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { issueKeys } from "../issues/queries";
import { useApplyEpicTickets } from "./epic";

function createWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

describe("useApplyEpicTickets", () => {
  let qc: QueryClient;
  let applyProjectEpicTickets: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    applyProjectEpicTickets = vi.fn().mockResolvedValue({ created_issue_ids: ["i1"] });
    setApiInstance({ applyProjectEpicTickets } as unknown as ApiClient);
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("invalidates this workspace's issue lists, not every workspace's", async () => {
    // A second, unrelated open workspace's issue query must stay untouched:
    // the bare ["issues"] key used to prefix-match and invalidate it too
    // (React Query invalidation matches by queryKey prefix).
    qc.setQueryData(issueKeys.all("ws-1"), []);
    qc.setQueryData(issueKeys.all("ws-2"), []);

    const { result } = renderHook(() => useApplyEpicTickets("ws-1", "project-1"), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync();
    });

    expect(qc.getQueryState(issueKeys.all("ws-1"))?.isInvalidated).toBe(true);
    expect(qc.getQueryState(issueKeys.all("ws-2"))?.isInvalidated).toBe(false);
  });
});
