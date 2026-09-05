import type { Goal } from "@multica/core/types";
import { goalChildren } from "@multica/core/goals";

export interface GoalTreeRow {
  goal: Goal;
  depth: number;
}

/**
 * Root-first, depth-first flattening of the goal tree for lists and pickers.
 * Cycle-safe: a goal already placed is never placed again, so a malformed
 * parent chain degrades to a truncated branch instead of a hang.
 */
export function flattenGoalTree(goals: Goal[]): GoalTreeRow[] {
  const rows: GoalTreeRow[] = [];
  const seen = new Set<string>();
  const walk = (parentId: string | null, depth: number) => {
    for (const goal of goalChildren(goals, parentId)) {
      if (seen.has(goal.id)) continue;
      seen.add(goal.id);
      rows.push({ goal, depth });
      walk(goal.id, depth + 1);
    }
  };
  walk(null, 0);
  // Orphans (parent missing from the list) still show, as roots.
  for (const goal of goals) {
    if (!seen.has(goal.id)) {
      seen.add(goal.id);
      rows.push({ goal, depth: 0 });
    }
  }
  return rows;
}
