// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import type { Goal } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({ goals: [] as Goal[], calls: [] as unknown[] }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));
vi.mock("@tanstack/react-query", () => ({ useQuery: () => ({ data: state.goals }) }));
vi.mock("@multica/core/goals", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/goals")>()),
  goalListOptions: () => ({ queryKey: ["goals"] }),
  useSetProjectGoals: () => ({ isPending: false, mutate: (v: unknown) => state.calls.push(v) }),
}));

import { ProjectGoalsSection } from "./project-goals-section";

const goal = (over: Partial<Goal>): Goal => ({
  id: "g", workspace_id: "ws-1", parent_goal_id: null, title: "Goal", description: "", success_measure: "",
  due_date: null, owner_id: null, status: "active", created_at: "", updated_at: "", issue_count: 2, done_count: 1, project_ids: [], ...over,
});

beforeEach(() => {
  state.goals = [goal({ id: "g1", title: "Grow revenue", project_ids: ["p1"] }), goal({ id: "g2", title: "Ship v2" })];
  state.calls = [];
});

describe("ProjectGoalsSection", () => {
  it("lists only the goals the project serves", () => {
    renderWithI18n(<ProjectGoalsSection projectId="p1" />);
    const rows = screen.getAllByTestId("project-goal");
    expect(rows.map((r) => r.textContent)).toEqual(["Grow revenueActive"]);
  });

  it("links a goal by sending the full desired list", async () => {
    renderWithI18n(<ProjectGoalsSection projectId="p1" />);
    fireEvent.click(screen.getByRole("button", { name: "Link goals" }));
    fireEvent.click(await screen.findByLabelText("Ship v2"));
    expect(state.calls[0]).toEqual({ projectId: "p1", goalIds: ["g1", "g2"] });
  });
});
