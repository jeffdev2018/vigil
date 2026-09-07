// @vitest-environment node

import { describe, expect, it } from "vitest";
import {
  BUG_FIX_RECIPE_STAGES,
  deriveBugFixRecipeReadiness,
  nextBugFixRecipeStage,
  type BugFixRecipeReadinessInput,
} from "./bugfix-recipe";

const ready: BugFixRecipeReadinessInput = {
  hasOnlineRuntime: true,
  cliAuth: "ready",
  hasAccessibleRepo: true,
  canInvokeAgent: true,
  hasDeliveryCriteria: true,
  hasHumanReviewer: true,
};

describe("deriveBugFixRecipeReadiness", () => {
  it("is ready when every required control passes", () => {
    const result = deriveBugFixRecipeReadiness(ready);
    expect(result.readyToStart).toBe(true);
    expect(result.blockedCount).toBe(0);
    expect(result.steps.map((s) => s.id)).toEqual([
      "runtime_online",
      "cli_authenticated_or_na",
      "repo_accessible",
      "agent_assignable",
      "delivery_criteria_ready",
      "human_reviewer_present",
    ]);
  });

  it("treats not_applicable CLI auth as unblocking", () => {
    const result = deriveBugFixRecipeReadiness({
      ...ready,
      cliAuth: "not_applicable",
    });
    expect(result.readyToStart).toBe(true);
    expect(
      result.steps.find((s) => s.id === "cli_authenticated_or_na")?.status,
    ).toBe("not_applicable");
  });

  it("blocks when any hard control fails", () => {
    for (const patch of [
      { hasOnlineRuntime: false },
      { cliAuth: "blocked" as const },
      { hasAccessibleRepo: false },
      { canInvokeAgent: false },
      { hasDeliveryCriteria: false },
      { hasHumanReviewer: false },
    ]) {
      const result = deriveBugFixRecipeReadiness({ ...ready, ...patch });
      expect(result.readyToStart).toBe(false);
      expect(result.blockedCount).toBeGreaterThan(0);
    }
  });

  it("counts unknown CLI auth as not ready", () => {
    const result = deriveBugFixRecipeReadiness({
      ...ready,
      cliAuth: "unknown",
    });
    expect(result.readyToStart).toBe(false);
  });
});

describe("nextBugFixRecipeStage", () => {
  it("walks reproduce → fix → test → open_pr → delivery_review", () => {
    expect(BUG_FIX_RECIPE_STAGES).toEqual([
      "reproduce",
      "fix",
      "test",
      "open_pr",
      "delivery_review",
    ]);
    expect(nextBugFixRecipeStage(null)).toBe("reproduce");
    expect(nextBugFixRecipeStage("reproduce")).toBe("fix");
    expect(nextBugFixRecipeStage("fix")).toBe("test");
    expect(nextBugFixRecipeStage("test")).toBe("open_pr");
    expect(nextBugFixRecipeStage("open_pr")).toBe("delivery_review");
    expect(nextBugFixRecipeStage("delivery_review")).toBe("delivery_review");
  });
});
