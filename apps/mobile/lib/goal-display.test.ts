// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Goal } from "@multica/core/types";
import { flattenGoalTree, goalProgress, goalStatusLabel } from "./goal-display";

const goal = (over: Partial<Goal>): Goal => ({
  id: "g",
  workspace_id: "w",
  parent_goal_id: null,
  title: "",
  description: "",
  success_measure: "",
  due_date: null,
  owner_id: null,
  status: "draft",
  created_at: "",
  updated_at: "",
  issue_count: 0,
  done_count: 0,
  project_ids: [],
  ...over,
});

describe("flattenGoalTree", () => {
  it("walks root-first with depth and keeps orphans", () => {
    const rows = flattenGoalTree([
      goal({ id: "c", parent_goal_id: "a" }),
      goal({ id: "a" }),
      goal({ id: "b" }),
      goal({ id: "d", parent_goal_id: "c" }),
      goal({ id: "orphan", parent_goal_id: "missing" }),
    ]);
    expect(rows.map((r) => [r.goal.id, r.depth])).toEqual([
      ["a", 0],
      ["c", 1],
      ["d", 2],
      ["b", 0],
      ["orphan", 0],
    ]);
  });

  it("emits each goal once on a cycle", () => {
    const rows = flattenGoalTree([
      goal({ id: "x", parent_goal_id: "y" }),
      goal({ id: "y", parent_goal_id: "x" }),
    ]);
    expect(rows.map((r) => r.goal.id).sort()).toEqual(["x", "y"]);
  });
});

it("goalProgress is 0 without issues and capped at 1", () => {
  expect(goalProgress(goal({}))).toBe(0);
  expect(goalProgress(goal({ issue_count: 4, done_count: 1 }))).toBe(0.25);
  expect(goalProgress(goal({ issue_count: 2, done_count: 5 }))).toBe(1);
});

it("goalStatusLabel falls back to the raw value", () => {
  expect(goalStatusLabel("active")).toBe("Active");
  expect(goalStatusLabel("weird")).toBe("weird");
});
