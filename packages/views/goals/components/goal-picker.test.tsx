// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Goal } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { PillButton } from "../../common/pill-button";

const goal = (over: Partial<Goal>): Goal => ({
  id: "g", workspace_id: "ws-1", parent_goal_id: null, title: "Goal", description: "", success_measure: "",
  due_date: null, owner_id: null, status: "draft", created_at: "", updated_at: "", issue_count: 0, done_count: 0, project_ids: [], ...over,
});

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: [goal({ id: "child", parent_goal_id: "root", title: "Ship v2" }), goal({ id: "root", title: "Grow revenue" })] }),
}));
vi.mock("@multica/core/goals", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/goals")>()),
  goalListOptions: () => ({ queryKey: ["goals"] }),
}));

import { GoalPicker } from "./goal-picker";

describe("GoalPicker", () => {
  it("selects a goal, root first", async () => {
    const user = userEvent.setup();
    const onUpdate = vi.fn();
    renderWithI18n(<GoalPicker goalId={null} onUpdate={onUpdate} triggerRender={<PillButton />} />);
    await user.click(screen.getByRole("button", { name: /no goal/i }));
    const items = await screen.findAllByRole("button", { name: /Grow revenue|Ship v2/ });
    expect(items.map((b) => b.textContent)).toEqual(["Grow revenue", "Ship v2"]);
    await user.click(items[1]!);
    expect(onUpdate).toHaveBeenCalledWith({ goal_id: "child" });
  });

  it("clears to inherit from the project", async () => {
    const user = userEvent.setup();
    const onUpdate = vi.fn();
    renderWithI18n(<GoalPicker goalId="root" onUpdate={onUpdate} triggerRender={<PillButton />} />);
    await user.click(screen.getByRole("button", { name: /Grow revenue/ }));
    await user.click(await screen.findByRole("button", { name: /no goal/i }));
    expect(onUpdate).toHaveBeenCalledWith({ goal_id: null });
  });
});
