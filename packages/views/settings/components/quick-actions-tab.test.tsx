// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({
  actions: [] as unknown[],
  actionsError: false,
  refetchActions: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({
    data: state.actions,
    isLoading: false,
    isError: state.actionsError,
    refetch: state.refetchActions,
  }),
}));
vi.mock("@multica/core/quick-actions", () => ({
  quickActionListOptions: (wsId: string) => ({ queryKey: ["quick-actions", wsId] }),
  useCreateQuickAction: () => ({ mutate: vi.fn(), mutateAsync: vi.fn(), isPending: false }),
  useUpdateQuickAction: () => ({ mutate: vi.fn(), mutateAsync: vi.fn(), isPending: false }),
  useDeleteQuickAction: () => ({ mutate: vi.fn(), mutateAsync: vi.fn(), isPending: false }),
}));

import { QuickActionsTab } from "./quick-actions-tab";

beforeEach(() => {
  state.actions = [];
  state.actionsError = false;
  state.refetchActions.mockClear();
});

// Regression: destructured only {data, isLoading} — a failed fetch fell
// through to the `filtered.length === 0` branch, the exact same "no quick
// actions yet" empty state (with its create CTA) as a workspace that
// genuinely has none.
describe("QuickActionsTab", () => {
  it("shows the create-CTA empty state when there really are no quick actions", () => {
    renderWithI18n(<QuickActionsTab />);
    expect(screen.getByText("No quick actions yet")).toBeInTheDocument();
  });

  it("shows an error state with retry instead of a false empty catalog when the fetch fails", () => {
    state.actionsError = true;
    renderWithI18n(<QuickActionsTab />);

    expect(screen.queryByText("No quick actions yet")).toBeNull();
    expect(screen.getByRole("alert")).toHaveTextContent("Could not load quick actions.");

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(state.refetchActions).toHaveBeenCalled();
  });
});
