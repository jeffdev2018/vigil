/**
 * Organisation browse page (K75). Read-only list of org structures in the
 * order the server returns them (workspace default first, then per
 * project). Each row folds its units.
 *
 * Title lives in the native iOS Stack header (Stack.Screen options in the
 * parent `_layout.tsx`). No `+` button: structures are authored on
 * web/desktop. No realtime hook: the server emits no `org:*` events, so
 * pull-to-refresh is the refresh path.
 */
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
import { OrgStructureRow } from "@/components/org/org-structure-row";
import { orgListOptions } from "@/data/queries/org";
import { findProject, projectListOptions } from "@/data/queries/projects";
import { useWorkspaceStore } from "@/data/workspace-store";

export default function OrgPage() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  const { data, isLoading, error, refetch, isRefetching } = useQuery(
    orgListOptions(wsId),
  );
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const structures = data ?? [];

  return (
    <SafeAreaView className="flex-1 bg-background" edges={[]}>
      {isLoading ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : error ? (
        <View className="px-4 gap-3 pt-4">
          <Text className="text-sm text-destructive">
            Failed to load organisation:{" "}
            {error instanceof Error ? error.message : "unknown error"}
          </Text>
          <Button variant="outline" onPress={() => refetch()}>
            <Text>Retry</Text>
          </Button>
        </View>
      ) : structures.length === 0 ? (
        <View className="flex-1 items-center justify-center px-6 gap-4">
          <Text className="text-base font-medium text-foreground">
            No org structure yet
          </Text>
          <Text className="text-sm text-muted-foreground text-center">
            Org structures are authored on web or desktop. Activate one to
            route issues through its units.
          </Text>
        </View>
      ) : (
        <FlatList
          data={structures}
          keyExtractor={(s) => s.id}
          ItemSeparatorComponent={() => (
            <View className="h-px bg-border ml-4" />
          )}
          renderItem={({ item }) => (
            <OrgStructureRow
              structure={item}
              projectName={
                item.project_id
                  ? (findProject(projects, item.project_id)?.title ??
                    "Project")
                  : null
              }
            />
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
