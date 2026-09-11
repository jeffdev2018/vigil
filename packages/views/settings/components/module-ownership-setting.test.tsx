// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import type { Workspace } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({
  rules: [] as unknown[],
  rulesError: false,
  refetchRules: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey: readonly unknown[] }) => {
    if (options.queryKey[0] === "module-ownership") {
      return { data: state.rules, isError: state.rulesError, refetch: state.refetchRules };
    }
    return { data: [] };
  },
}));
vi.mock("@multica/core/issues/ownership", () => ({
  moduleOwnershipOptions: (wsId: string) => ({ queryKey: ["module-ownership", wsId] }),
  useCreateModuleOwnership: () => ({ mutate: vi.fn() }),
  useDeleteModuleOwnership: () => ({ mutate: vi.fn() }),
}));
vi.mock("@multica/core/labels/queries", () => ({
  labelListOptions: (wsId: string) => ({ queryKey: ["labels", wsId] }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: (wsId: string) => ({ queryKey: ["agents", wsId] }),
  memberListOptions: (wsId: string) => ({ queryKey: ["members", wsId] }),
}));

import { ModuleOwnershipSetting } from "./module-ownership-setting";

const workspace = { id: "ws-1" } as Workspace;

beforeEach(() => {
  state.rules = [];
  state.rulesError = false;
  state.refetchRules.mockClear();
});

// Regression: rules/members/agents/labels all defaulted to `?? []` with no
// isError read — a failed fetch rendered the exact same "no rules yet" empty
// state as a workspace with none configured.
describe("ModuleOwnershipSetting", () => {
  it("shows the empty state when there really are no rules", () => {
    renderWithI18n(<ModuleOwnershipSetting workspace={workspace} canEdit />);
    expect(screen.getByTestId("ownership-empty")).toBeInTheDocument();
  });

  it("shows an error state with retry instead of a false empty list when the fetch fails", () => {
    state.rulesError = true;
    renderWithI18n(<ModuleOwnershipSetting workspace={workspace} canEdit />);

    expect(screen.queryByTestId("ownership-empty")).toBeNull();
    expect(screen.getByRole("alert")).toHaveTextContent("Could not load module ownership rules.");

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(state.refetchRules).toHaveBeenCalled();
  });
});
