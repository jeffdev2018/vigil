/**
 * Goal tree display helpers (K74).
 *
 * `goalChildren` / `goalProgress` mirror `packages/core/goals/queries.ts`.
 * Mirrored, not imported: that file also exports React Query hooks, which
 * are outside mobile's `@multica/core` whitelist (types + pure functions).
 * When the web version changes, sync this file.
 */
import type { Goal, GoalStatus } from "@multica/core/types";

/** Children of a goal, in creation order. Root goals have a null parent. */
export function goalChildren(goals: Goal[], parentId: string | null): Goal[] {
  return goals.filter((g) => g.parent_goal_id === parentId);
}

/** Done ratio in [0, 1]; 0 without issues. */
export function goalProgress(
  goal: Pick<Goal, "issue_count" | "done_count">,
): number {
  return goal.issue_count > 0
    ? Math.min(1, goal.done_count / goal.issue_count)
    : 0;
}

export interface GoalTreeRow {
  goal: Goal;
  depth: number;
}

/**
 * Root-first depth-first flattening for a FlatList, with the same cycle
 * guard the web tree uses (a goal already emitted is never emitted again).
 * Orphans (parent id not in the list) are appended as roots so no goal is
 * silently dropped.
 */
export function flattenGoalTree(goals: Goal[]): GoalTreeRow[] {
  const rows: GoalTreeRow[] = [];
  const seen = new Set<string>();
  const walk = (parentId: string | null, depth: number) => {
    for (const g of goalChildren(goals, parentId)) {
      if (seen.has(g.id)) continue;
      seen.add(g.id);
      rows.push({ goal: g, depth });
      walk(g.id, depth + 1);
    }
  };
  walk(null, 0);
  for (const g of goals) {
    if (!seen.has(g.id)) {
      seen.add(g.id);
      rows.push({ goal: g, depth: 0 });
      walk(g.id, 1);
    }
  }
  return rows;
}

export const GOAL_STATUS_LABEL: Record<GoalStatus, string> = {
  draft: "Draft",
  active: "Active",
  done: "Done",
  dropped: "Dropped",
};

// Unknown server values render as-is rather than crashing or vanishing
// (root CLAUDE.md "API Response Compatibility").
export function goalStatusLabel(value: string): string {
  return (GOAL_STATUS_LABEL as Record<string, string>)[value] ?? value;
}
