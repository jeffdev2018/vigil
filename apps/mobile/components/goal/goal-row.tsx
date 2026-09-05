/**
 * Goal tree row (K74). Read-only. Mirrors `ProjectRow`'s layout shape
 * (flex title + right column) and reuses the progress bar shape from
 * `ProjectHeaderCard`'s ProgressSection. Depth indents the row, which is
 * how the web goals page renders the tree.
 *
 * Layout:
 *   [indent] Goal title                          [3/12]
 *            [Active] [Due Sep 30]               [====----]
 *            success measure (secondary)
 */
import { View } from "react-native";
import type { Goal } from "@multica/core/types";
import { formatDateOnly } from "@multica/core/issues/date";
import { Text } from "@/components/ui/text";
import { goalProgress, goalStatusLabel } from "@/lib/goal-display";

const INDENT_PX = 20;

interface Props {
  goal: Goal;
  depth: number;
}

export function GoalRow({ goal, depth }: Props) {
  const pct = Math.round(goalProgress(goal) * 100);
  // due_date is a calendar day — format timezone-safely (same helper as
  // the issue attribute row) so the day never shifts with the viewer's offset.
  const due = goal.due_date
    ? formatDateOnly(goal.due_date, { month: "short", day: "numeric" }, "en-US")
    : "";

  return (
    <View
      className="px-4 py-3 gap-1.5"
      style={{ paddingLeft: 16 + depth * INDENT_PX }}
    >
      <View className="flex-row items-start gap-3">
        <View className="flex-1 gap-1">
          <Text
            className="text-base text-foreground font-medium"
            numberOfLines={2}
          >
            {goal.title}
          </Text>
          <View className="flex-row items-center gap-3">
            <Text className="text-xs text-muted-foreground">
              {goalStatusLabel(goal.status)}
            </Text>
            {due ? (
              <Text className="text-xs text-muted-foreground">Due {due}</Text>
            ) : null}
          </View>
        </View>
        {goal.issue_count > 0 ? (
          <Text className="text-xs text-muted-foreground tabular-nums">
            {goal.done_count}/{goal.issue_count}
          </Text>
        ) : (
          <Text className="text-xs text-muted-foreground/60">—</Text>
        )}
      </View>
      <View className="h-1.5 bg-secondary rounded-full overflow-hidden">
        <View
          className="h-full bg-brand rounded-full"
          style={{ width: `${pct}%` }}
        />
      </View>
      {goal.success_measure ? (
        <Text className="text-xs text-muted-foreground" numberOfLines={2}>
          {goal.success_measure}
        </Text>
      ) : null}
    </View>
  );
}
