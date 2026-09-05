// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { goalAncestry, goalChildren, goalProgress } from "./queries";
import type { Goal } from "../types";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

const goal = (over: Partial<Goal>): Goal => ({
  id: "g", workspace_id: "w", parent_goal_id: null, title: "", description: "", success_measure: "", due_date: null, owner_id: null,
  status: "draft", created_at: "", updated_at: "", issue_count: 0, done_count: 0, project_ids: [], ...over,
});

describe("goals client", () => {
  it("parses a goal list tolerantly and falls back on garbage", async () => {
    stubFetch({ goals: [{ id: "g1", title: "Mission", status: "weird", issue_count: "3" }], total: 1 });
    const list = await new ApiClient("https://api.example.test").listGoals();
    expect(list.goals[0]?.status).toBe("draft");
    expect(list.goals[0]?.issue_count).toBe(0);
    expect(list.goals[0]?.project_ids).toEqual([]);
    stubFetch("garbage");
    expect((await new ApiClient("https://api.example.test").listGoals()).goals).toEqual([]);
    stubFetch({ goal: { id: "g1", title: "Mission" }, issues: "nope" });
    const detail = await new ApiClient("https://api.example.test").getGoal("g1");
    expect(detail.goal?.id).toBe("g1");
    expect(detail.issues).toEqual([]);
    stubFetch({ goal_ids: ["g1"] });
    expect(await new ApiClient("https://api.example.test").setProjectGoals("p1", ["g1"])).toEqual(["g1"]);
  });

  it("walks the tree and computes progress", () => {
    const goals = [goal({ id: "m" }), goal({ id: "s", parent_goal_id: "m" }), goal({ id: "t", parent_goal_id: "s" })];
    expect(goalChildren(goals, null).map((g) => g.id)).toEqual(["m"]);
    expect(goalAncestry(goals, "t").map((g) => g.id)).toEqual(["m", "s", "t"]);
    expect(goalAncestry([goal({ id: "a", parent_goal_id: "b" }), goal({ id: "b", parent_goal_id: "a" })], "a").length).toBe(2);
    expect(goalProgress(goal({ issue_count: 4, done_count: 1 }))).toBe(0.25);
    expect(goalProgress(goal({}))).toBe(0);
  });
});
