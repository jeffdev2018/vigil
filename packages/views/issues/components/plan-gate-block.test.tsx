// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import type { IssuePlan } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Stage math and schema fallbacks: packages/core/issues/plan-gate.test.ts.

const state = vi.hoisted(() => ({ materialize: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issues/plan", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/plan")>()),
  useMaterializeIssuePlan: () => ({ mutate: state.materialize, isPending: false }),
}));

import { PlanGateBlock } from "./plan-gate-block";

const plan = (over: Partial<IssuePlan> = {}): IssuePlan => ({
  id: "p1", issue_id: "a", version: 2, content: "Plan", author_type: "agent", author_id: "ag",
  superseded_at: null, materialized_at: null, created_at: "2026-09-03T00:00:00Z",
  steps: [{ id: "s1", title: "Add the endpoint" }, { id: "s2", title: "Test it", after: ["s1"] }],
  ...over,
});

beforeEach(() => state.materialize.mockReset());

describe("PlanGateBlock", () => {
  it("explains that a plan without steps creates nothing", () => {
    renderWithI18n(<PlanGateBlock issueId="a" plan={plan({ steps: [] })} />);
    expect(screen.getByText("No structured steps: nothing to create as sub-issues.")).toBeTruthy();
  });

  it("lists staged steps and approves the shown version", () => {
    renderWithI18n(<PlanGateBlock issueId="a" plan={plan()} />);
    expect(screen.getAllByRole("listitem").map((li) => li.textContent)).toEqual(["1Add the endpoint", "2Test it"]);
    fireEvent.click(screen.getByRole("button", { name: "Approve: create 2 sub-issues" }));
    expect(state.materialize).toHaveBeenCalledWith({ issueId: "a", version: 2 }, expect.anything());
  });

  it("shows the created sub-issues instead of the button once materialized", () => {
    renderWithI18n(
      <PlanGateBlock
        issueId="a"
        plan={plan({ materialized_at: "2026-09-03T01:00:00Z", steps: [{ id: "s1", title: "Add the endpoint", issue_id: "c1" }] })}
      />,
    );
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.getByTestId("plan-gate").dataset.materialized).toBe("true");
    expect(screen.getByText(/1 sub-issues created/)).toBeTruthy();
  });

  it("offers no approval on a superseded version", () => {
    renderWithI18n(<PlanGateBlock issueId="a" plan={plan({ superseded_at: "2026-09-03T02:00:00Z" })} />);
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.getByText("Superseded: approve the active version instead.")).toBeTruthy();
  });
});
