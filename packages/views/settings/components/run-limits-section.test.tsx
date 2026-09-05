// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { RunLimitPolicy } from "@multica/core/budgets/run-limits";
import { renderWithI18n } from "../../test/i18n";

// Parsing and formatting: packages/core/budgets/run-limits.test.ts.

const state = vi.hoisted(() => ({ policies: [] as RunLimitPolicy[], save: vi.fn(), remove: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/workspace/queries", () => ({ agentListOptions: () => ({ queryKey: ["agents"], queryFn: async () => [{ id: "a1", name: "Builder" }] }) }));
vi.mock("@multica/core/projects", () => ({ projectListOptions: () => ({ queryKey: ["projects"], queryFn: async () => [{ id: "p1", title: "Billing" }] }) }));
vi.mock("@multica/core/budgets/run-limits", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/budgets/run-limits")>()),
  runLimitPoliciesOptions: () => ({ queryKey: ["rl"], queryFn: async () => state.policies }),
  useSaveRunLimitPolicy: () => ({ mutate: state.save, isPending: false }),
  useDeleteRunLimitPolicy: () => ({ mutate: state.remove, isPending: false }),
}));

import { RunLimitsSection } from "./run-limits-section";

function render(canManage = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <RunLimitsSection canManage={canManage} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.policies = [{ id: "r1", scope_type: "agent", scope_id: "a1", max_cost_usd_ticks: 20000000000, max_duration_seconds: 1800, max_turns: null, max_tool_calls: null, warn_bps: 8000, action: "enforce", created_at: "" }];
  state.save.mockReset();
  state.remove.mockReset();
});

describe("RunLimitsSection", () => {
  it("lists caps per scope and creates a new policy", async () => {
    render();
    expect(await screen.findByText("Agent · Builder")).toBeTruthy();
    expect(screen.getByText("Cost ≤ $2.00")).toBeTruthy();
    expect(screen.getByText("Duration ≤ 30m00s")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "New run limit" }));
    fireEvent.change(screen.getByLabelText("Turns"), { target: { value: "25" } });
    const selects = screen.getByTestId("run-limit-editor").querySelectorAll("select");
    fireEvent.change(selects[selects.length - 1] as HTMLSelectElement, { target: { value: "observe" } });
    fireEvent.click(screen.getByRole("button", { name: "Save run limit" }));
    expect(state.save).toHaveBeenCalledWith({ input: { scope_type: "workspace", scope_id: null, max_cost_usd_ticks: null, max_duration_seconds: null, max_turns: 25, max_tool_calls: null, warn_bps: 8000, action: "observe" } }, expect.anything());
  });

  it("deletes a policy", async () => {
    render();
    fireEvent.click(await screen.findByLabelText("Delete run limit for Agent · Builder"));
    expect(state.remove).toHaveBeenCalledWith("r1", expect.anything());
  });

  it("stays read-only for members", async () => {
    render(false);
    expect(await screen.findByText("Agent · Builder")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "New run limit" })).toBeNull();
    expect(screen.queryByLabelText("Delete run limit for Agent · Builder")).toBeNull();
  });
});
