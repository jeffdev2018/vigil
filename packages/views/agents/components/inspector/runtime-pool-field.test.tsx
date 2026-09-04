// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import type { RuntimePool } from "@multica/core/runtimes/pools";
import { renderWithI18n } from "../../../test/i18n";

const state = vi.hoisted(() => ({ pools: [] as RuntimePool[], assign: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/runtimes/pools", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/runtimes/pools")>()),
  runtimePoolsOptions: () => ({ queryKey: ["pools"], queryFn: async () => state.pools }),
  useSetAgentRuntimePool: () => ({ mutate: state.assign, isPending: false }),
}));

import { RuntimePoolField } from "./runtime-pool-field";

function render(agent: Partial<Agent>, canEdit = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <RuntimePoolField agent={{ id: "a1", name: "Builder", ...agent } as Agent} canEdit={canEdit} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.pools = [{ id: "p1", name: "main", runtime_ids: ["a", "b"], degraded_runtime_id: null, agent_count: 1, created_at: "" }];
  state.assign.mockReset();
});

describe("RuntimePoolField", () => {
  it("shows the current pool read-only and assigns from the picker", async () => {
    render({ runtime_pool_id: "p1" }, false);
    expect(await screen.findByText("main")).toBeTruthy();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("assigns and clears", async () => {
    render({ runtime_pool_id: null });
    fireEvent.click(await screen.findByRole("button", { name: /Runtime pool: No pool/ }));
    fireEvent.click(await screen.findByText("2 runtimes in order"));
    expect(state.assign).toHaveBeenCalledWith("p1", expect.anything());
  });
});
