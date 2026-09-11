/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { setCurrentWorkspace } from "../platform/workspace-storage";
import {
  getIssueSurfaceViewStore,
  pruneIssueSurfaceViewStates,
} from "../issues/stores/surface-view-store";
import { useDeleteProject } from "./mutations";
import { projectKeys } from "./queries";
import type { ListProjectsResponse } from "../types";

vi.mock("../hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

function createWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

describe("useDeleteProject", () => {
  let qc: QueryClient;
  let deleteProject: ReturnType<typeof vi.fn<() => Promise<void>>>;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    deleteProject = vi.fn().mockResolvedValue(undefined);
    setApiInstance({ deleteProject } as unknown as ApiClient);
    setCurrentWorkspace("acme", "ws-1");
  });

  afterEach(() => {
    qc.clear();
    pruneIssueSurfaceViewStates([]);
    setCurrentWorkspace(null, null);
    vi.restoreAllMocks();
  });

  it("clears the deleted project's issue surface view state", async () => {
    const store = getIssueSurfaceViewStore("project:p1");
    store.getState().setViewMode("list");
    expect(store.getState().viewMode).toBe("list");

    const { result } = renderHook(() => useDeleteProject(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync("p1");
    });

    expect(deleteProject).toHaveBeenCalledWith("p1");
    expect(store.getState().viewMode).toBe("board");
  });

  // JEF-397: a delete awaits the server, so a refusal leaves the list intact
  // rather than rolling back a removal that never should have happened.
  it("keeps the project in the list when the server refuses", async () => {
    deleteProject.mockRejectedValue(new Error("403"));
    const list = {
      projects: [{ id: "p1" }, { id: "p2" }],
      total: 2,
    } as unknown as ListProjectsResponse;
    qc.setQueryData(projectKeys.list("ws-1"), list);

    const { result } = renderHook(() => useDeleteProject(), {
      wrapper: createWrapper(qc),
    });
    await act(async () => {
      await result.current.mutateAsync("p1").catch(() => undefined);
    });

    expect(qc.getQueryData<ListProjectsResponse>(projectKeys.list("ws-1"))?.total).toBe(2);
  });
});
