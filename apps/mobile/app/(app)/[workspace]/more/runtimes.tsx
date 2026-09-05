/**
 * Runtimes — read-only machine list.
 *
 * Rows are machines (the daemon and every CLI it registered), not individual
 * runtime rows: "how many of my CLIs are up on this laptop" is the question a
 * phone is for, and it is how web's runtimes page groups them too
 * (packages/views/runtimes/components/runtime-machines.ts). Mobile keeps only
 * the grouping — no workload summaries, no section split, no synthetic
 * "this machine" row, since none of those render here.
 *
 * No actions: renaming, deleting, updating, model listing and CLI sign-in all
 * belong to the machine the daemon runs on, not to a phone.
 *
 * Feeds off the same `runtimeListOptions` the presence dot already uses, so
 * opening this screen costs no extra request in a warm session.
 */
import { useMemo } from "react";
import { ActivityIndicator, FlatList, Pressable, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { router } from "expo-router";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { runtimeListOptions } from "@/data/queries/runtimes";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  groupRuntimesByMachine,
  type RuntimeMachine,
} from "@/lib/runtime-display";
import { timeAgo } from "@/lib/time-ago";
import { cn } from "@/lib/utils";

export default function RuntimesPage() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);

  const { data, isLoading, error, refetch, isRefetching } = useQuery(
    runtimeListOptions(wsId),
  );

  const machines = useMemo(
    () => groupRuntimesByMachine(data ?? []),
    [data],
  );

  if (isLoading) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator />
      </View>
    );
  }

  if (error) {
    return (
      <View className="flex-1 bg-background px-4 gap-3 pt-4">
        <Text className="text-sm text-destructive">
          Could not load runtimes:{" "}
          {error instanceof Error ? error.message : "unknown error"}
        </Text>
        <Button variant="outline" onPress={() => refetch()}>
          <Text>Retry</Text>
        </Button>
      </View>
    );
  }

  return (
    <FlatList
      className="flex-1 bg-background"
      data={machines}
      keyExtractor={(machine) => machine.id}
      ItemSeparatorComponent={() => <View className="h-px bg-border ml-4" />}
      contentContainerClassName="pb-6"
      ListEmptyComponent={
        <Text className="px-6 py-12 text-center text-sm text-muted-foreground">
          No runtimes in this workspace. Start the Multica daemon on a machine
          with an agent CLI installed and it registers itself here.
        </Text>
      }
      refreshing={isRefetching}
      onRefresh={refetch}
      renderItem={({ item }) => (
        <MachineRow
          machine={item}
          onPress={() => {
            if (wsSlug) router.push(`/${wsSlug}/runtime/${item.id}`);
          }}
        />
      )}
    />
  );
}

function MachineRow({
  machine,
  onPress,
}: {
  machine: RuntimeMachine;
  onPress: () => void;
}) {
  const total = machine.runtimes.length;
  const online = machine.onlineCount > 0;

  return (
    <Pressable
      onPress={onPress}
      className="flex-row items-center gap-3 px-4 py-3 active:bg-secondary"
      accessibilityLabel={machine.title}
    >
      <View
        accessibilityLabel={online ? "Online" : "Offline"}
        className={cn(
          "size-2 rounded-full",
          online ? "bg-emerald-500" : "bg-muted-foreground/40",
        )}
      />
      <View className="flex-1 min-w-0 gap-0.5">
        <Text className="text-sm text-foreground" numberOfLines={1}>
          {machine.title}
        </Text>
        <Text className="text-xs text-muted-foreground" numberOfLines={1}>
          {[
            `${machine.onlineCount}/${total} online`,
            machine.cliVersion ? `CLI ${machine.cliVersion}` : null,
            machine.lastSeenAt ? `Seen ${timeAgo(machine.lastSeenAt)}` : null,
          ]
            .filter(Boolean)
            .join(" · ")}
        </Text>
      </View>
    </Pressable>
  );
}
