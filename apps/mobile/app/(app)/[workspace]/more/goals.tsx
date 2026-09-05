/**
 * Goals browse page (K74). Read-only tree, root-first, children indented
 * by depth — same ordering as the web goals page (`flattenGoalTree` in
 * `lib/goal-display.ts` mirrors `goalChildren` from
 * `packages/core/goals/queries.ts`).
 *
 * Title lives in the native iOS Stack header (Stack.Screen options in the
 * parent `_layout.tsx`). No `+` button: goals are created on web/desktop.
 * No realtime hook: the server emits no `goal:*` events yet, so
 * pull-to-refresh is the refresh path.
 */
import { useMemo } from "react";
import {
  ActivityIndicator,
  FlatList,
  RefreshControl,
  View,
} from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { GoalRow } from "@/components/goal/goal-row";
import { goalListOptions } from "@/data/queries/goals";
import { useWorkspaceStore } from "@/data/workspace-store";
import { flattenGoalTree } from "@/lib/goal-display";

export default function GoalsPage() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  const { data, isLoading, error, refetch, isRefetching } = useQuery(
    goalListOptions(wsId),
  );

  const rows = useMemo(() => flattenGoalTree(data ?? []), [data]);

  return (
    <SafeAreaView className="flex-1 bg-background" edges={[]}>
      {isLoading ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : error ? (
        <View className="px-4 gap-3 pt-4">
          <Text className="text-sm text-destructive">
            Failed to load goals:{" "}
            {error instanceof Error ? error.message : "unknown error"}
          </Text>
          <Button variant="outline" onPress={() => refetch()}>
            <Text>Retry</Text>
          </Button>
        </View>
      ) : rows.length === 0 ? (
        <View className="flex-1 items-center justify-center px-6 gap-4">
          <Text className="text-base font-medium text-foreground">
            No goals yet
          </Text>
          <Text className="text-sm text-muted-foreground text-center">
            Goals are created on web or desktop. Link projects to a goal to
            roll their progress up here.
          </Text>
        </View>
      ) : (
        <FlatList
          data={rows}
          keyExtractor={(row) => row.goal.id}
          ItemSeparatorComponent={() => (
            <View className="h-px bg-border ml-4" />
          )}
          renderItem={({ item }) => (
            <GoalRow goal={item.goal} depth={item.depth} />
          )}
          refreshControl={
            <RefreshControl refreshing={isRefetching} onRefresh={refetch} />
          }
          contentContainerClassName="pb-6"
        />
      )}
    </SafeAreaView>
  );
}
