// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, within } from "@testing-library/react";
import type { Goal } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Tree flattening and progress math: goal-tree.ts / packages/core/goals/queries.test.ts.

const state = vi.hoisted(() => ({
  goals: [] as Goal[],
  created: [] as unknown[],
  deleteError: null as Error | null,
  toastError: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }) }));
vi.mock("@multica/core/auth", () => ({ useAuthStore: (sel: (s: unknown) => unknown) => sel({ user: { id: "u-1" } }) }));
vi.mock("@multica/core/workspace/queries", () => ({ memberListOptions: () => ({ queryKey: ["members"] }) }));
vi.mock("sonner", () => ({ toast: { error: state.toastError } }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: (o: { queryKey?: readonly unknown[] }) => {
    const key = o.queryKey?.[0];
    if (key === "goals") return { data: state.goals, isLoading: false, isPending: false };
    if (key === "members") return { data: [{ user_id: "u-1", name: "Ada", role: "owner" }], isLoading: false };
    return { data: undefined, isLoading: false, isPending: true };
  },
}));
vi.mock("@multica/core/goals", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/goals")>();
  return {
    ...actual,
    goalListOptions: () => ({ queryKey: ["goals"] }),
    goalDetailOptions: (_ws: string, id: string) => ({ queryKey: ["goal", id] }),
    useCreateGoal: () => ({ isPending: false, mutate: (data: unknown, o: { onSuccess: () => void }) => { state.created.push(data); o.onSuccess(); } }),
    useUpdateGoal: () => ({ isPending: false, mutate: vi.fn() }),
    useDeleteGoal: () => ({
      isPending: false,
      mutate: (_id: string, o: { onError: (e: unknown) => void; onSettled: () => void }) => {
        if (state.deleteError) o.onError(state.deleteError);
        o.onSettled();
      },
    }),
  };
});

import { GoalsPage } from "./goals-page";

const goal = (over: Partial<Goal>): Goal => ({
  id: "g", workspace_id: "ws-1", parent_goal_id: null, title: "Goal", description: "", success_measure: "",
  due_date: null, owner_id: null, status: "draft", created_at: "", updated_at: "", issue_count: 0, done_count: 0, project_ids: [], ...over,
});

beforeEach(() => {
  state.goals = [];
  state.created = [];
  state.deleteError = null;
  state.toastError.mockReset();
});

describe("GoalsPage", () => {
  it("renders the root before its child, indented, with progress and owner", () => {
    state.goals = [
      goal({ id: "child", parent_goal_id: "root", title: "Ship v2", issue_count: 4, done_count: 1 }),
      goal({ id: "root", title: "Grow revenue", owner_id: "u-1", status: "active", issue_count: 10, done_count: 5, success_measure: "ARR x2" }),
    ];
    renderWithI18n(<GoalsPage />);
    const rows = screen.getAllByTestId("goal-row");
    expect(rows.map((r) => r.getAttribute("data-depth"))).toEqual(["0", "1"]);
    expect(rows[0]?.textContent).toContain("Grow revenue");
    expect(rows[0]?.textContent).toContain("5 / 10 done");
    expect(rows[0]?.textContent).toContain("Ada");
    expect(rows[0]?.textContent).toContain("ARR x2");
    expect(rows[1]?.textContent).toContain("1 / 4 done");
  });

  it("shows the empty state without goals", () => {
    renderWithI18n(<GoalsPage />);
    expect(screen.getByText("No goals yet")).toBeInTheDocument();
  });

  it("creates a sub-goal with the row's goal preselected as parent", async () => {
    state.goals = [goal({ id: "root", title: "Grow revenue" })];
    renderWithI18n(<GoalsPage />);
    fireEvent.click(screen.getByRole("button", { name: "Add sub-goal" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Title"), { target: { value: "Ship v2" } });
    expect((within(dialog).getByLabelText("Parent goal") as HTMLSelectElement).value).toBe("root");
    fireEvent.click(within(dialog).getByRole("button", { name: "Create goal" }));
    expect(state.created[0]).toMatchObject({ title: "Ship v2", parent_goal_id: "root", status: "draft", owner_id: null });
  });

  it("surfaces the server's refusal when deleting a goal with sub-goals", async () => {
    state.goals = [goal({ id: "root", title: "Grow revenue" })];
    state.deleteError = new Error("goal still has sub-goals");
    renderWithI18n(<GoalsPage />);
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));
    expect(state.toastError).toHaveBeenCalledWith("goal still has sub-goals");
  });
});
