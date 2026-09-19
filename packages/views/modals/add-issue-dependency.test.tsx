// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { renderWithI18n } from "../test/i18n";
import { AddIssueDependencyModal } from "./add-issue-dependency";
import type { Issue } from "@multica/core/types";

// R01: ONE entry point covers the four relation types — a type selector rides
// above the picker, the mutation carries the chosen type, and a preset type
// (from a direct menu path) preselects it.

const state = vi.hoisted(() => ({
  mutate: vi.fn(),
  target: null as Issue | null,
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issues/dependencies", () => ({
  useAddIssueDependency: () => ({ mutate: state.mutate }),
}));
vi.mock("./issue-picker-modal", () => ({
  IssuePickerModal: ({
    above,
    onSelect,
  }: {
    above?: React.ReactNode;
    onSelect: (issue: Issue) => void;
  }) => (
    <div>
      <div data-testid="picker">{above}</div>
      <button type="button" onClick={() => state.target && onSelect(state.target)}>
        pick-target
      </button>
    </div>
  ),
}));

beforeEach(() => {
  state.mutate.mockReset();
  state.target = {
    id: "t1", workspace_id: "ws-1", number: 7, identifier: "MUL-7", title: "Target",
    status: "todo", description: null, priority: "none", assignee_type: null, assignee_id: null,
    creator_type: "member", creator_id: "u", parent_issue_id: null, project_id: null,
    position: 0, start_date: null, due_date: null, created_at: "", updated_at: "",
  } as Issue;
});

function renderModal(data: Record<string, unknown> | null = { issueId: "a" }) {
  return renderWithI18n(<AddIssueDependencyModal onClose={vi.fn()} data={data} />);
}

describe("AddIssueDependencyModal", () => {
  it("offers all four relation types and defaults to blocks", () => {
    renderModal();
    const selector = screen.getByTestId("relation-type-selector");
    for (const label of ["Blocks", "Blocked by", "Related to", "Duplicate of"]) {
      expect(selector.textContent).toContain(label);
    }
    expect(screen.getByRole("button", { name: "Blocks" }).getAttribute("aria-pressed")).toBe("true");
  });

  it("sends the chosen type with the mutation", () => {
    renderModal();
    fireEvent.click(screen.getByRole("button", { name: "Duplicate of" }));
    fireEvent.click(screen.getByRole("button", { name: "pick-target" }));
    expect(state.mutate).toHaveBeenCalledWith(
      expect.objectContaining({ issueId: "a", targetIssueId: "t1", type: "duplicate" }),
      expect.anything(),
    );
  });

  it("preselects a preset type from the caller", () => {
    renderModal({ issueId: "a", type: "blocked_by" });
    expect(screen.getByRole("button", { name: "Blocked by" }).getAttribute("aria-pressed")).toBe("true");
  });

  it("falls back to blocks on an unknown preset", () => {
    renderModal({ issueId: "a", type: "nonsense" });
    expect(screen.getByRole("button", { name: "Blocks" }).getAttribute("aria-pressed")).toBe("true");
  });
});
