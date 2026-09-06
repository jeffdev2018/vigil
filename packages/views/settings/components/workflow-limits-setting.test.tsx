// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({ save: vi.fn() }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/agents/routing-check", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/agents/routing-check")>()),
  workflowLimitsOptions: () => ({
    queryKey: ["workflow-limits"],
    queryFn: async () => ({ max_legs: 8, max_cost_usd_ticks: 0, min_legs: 1, max_legs_allowed: 50 }),
  }),
  useSaveWorkflowLimits: () => ({ mutate: state.save, isPending: false }),
}));

import { WorkflowLimitsSetting } from "./workflow-limits-setting";

describe("WorkflowLimitsSetting", () => {
  it("clamps a leg count to the range the server reported and saves both fields together", async () => {
    state.save.mockClear();
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    renderWithI18n(
      <QueryClientProvider client={qc}>
        <WorkflowLimitsSetting canEdit />
      </QueryClientProvider>,
    );
    const legs = (await screen.findByLabelText("Maximum runs per workflow")) as HTMLInputElement;
    await waitFor(() => expect(legs.value).toBe("8"));

    fireEvent.change(legs, { target: { value: "0" } });
    fireEvent.blur(legs);
    // Clamped up to min_legs, and the untouched cost travels with it: the
    // endpoint replaces the whole object.
    expect(state.save).toHaveBeenCalledWith({ max_legs: 1, max_cost_usd_ticks: 0 }, expect.anything());

    fireEvent.change(legs, { target: { value: "999" } });
    fireEvent.blur(legs);
    expect(state.save).toHaveBeenLastCalledWith({ max_legs: 50, max_cost_usd_ticks: 0 }, expect.anything());

    // A negative cost clamps to 0, which is what is already stored, so there
    // is nothing to save and the field shows the clamped value.
    const cost = screen.getByLabelText("Maximum cost per workflow") as HTMLInputElement;
    const callsBefore = state.save.mock.calls.length;
    fireEvent.change(cost, { target: { value: "-5" } });
    fireEvent.blur(cost);
    expect(cost.value).toBe("0");
    expect(state.save.mock.calls.length).toBe(callsBefore);

    fireEvent.change(cost, { target: { value: "9000" } });
    fireEvent.blur(cost);
    expect(state.save).toHaveBeenLastCalledWith({ max_legs: 8, max_cost_usd_ticks: 9000 }, expect.anything());
  });

  it("does not save an unchanged value", async () => {
    state.save.mockClear();
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    renderWithI18n(
      <QueryClientProvider client={qc}>
        <WorkflowLimitsSetting canEdit />
      </QueryClientProvider>,
    );
    const legs = (await screen.findByLabelText("Maximum runs per workflow")) as HTMLInputElement;
    await waitFor(() => expect(legs.value).toBe("8"));
    fireEvent.blur(legs);
    expect(state.save).not.toHaveBeenCalled();
  });
});
