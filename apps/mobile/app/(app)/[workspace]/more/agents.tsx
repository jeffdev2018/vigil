/**
 * Agents — read-only list.
 *
 * Its point today is surfacing `runtime_routing` (JEF-237): whether an agent
 * is pinned to one runtime ("Fixed") or lets the router pick per task
 * ("Auto"). Web fuses that control into the runtime picker
 * (packages/views/agents/components/agent-detail-inspector.tsx — choosing Auto
 * is deliberately NOT a runtime change and preserves model / thinking level /
 * service tier). Mobile has no agent editor to fuse it into, so the mode is
 * displayed, not edited; a toggle here would have to re-implement that whole
 * rule set and would be the only writable agent field on the platform.
 *
 * Parity note: `runtime_routing` is absent on an older backend, and BOTH
 * clients must read `undefined` as "fixed" (see the field comment in
 * packages/core/types/agent.ts). Never render "Auto" for a missing value.
 */
import { useMemo } from "react";
import { ActivityIndicator, FlatList, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { agentListOptions } from "@/data/queries/agents";
import { runtimeListOptions } from "@/data/queries/runtimes";
import { useWorkspaceStore } from "@/data/workspace-store";
import { runtimeDisplayName } from "@/lib/runtime-display";

export default function AgentsPage() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  const { data, isLoading, error, refetch, isRefetching } = useQuery(
    agentListOptions(wsId),
  );
  const { data: runtimes } = useQuery(runtimeListOptions(wsId));

  const runtimeNames = useMemo(() => {
    const map = new Map<string, string>();
    for (const runtime of runtimes ?? []) {
      map.set(runtime.id, runtimeDisplayName(runtime));
    }
    return map;
  }, [runtimes]);

  // Archived agents are not part of the working set on any client.
  const agents = useMemo(
    () => (data ?? []).filter((a) => !a.archived_at),
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
          Could not load agents:{" "}
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
      data={agents}
      keyExtractor={(agent) => agent.id}
      ItemSeparatorComponent={() => <View className="h-px bg-border ml-4" />}
      contentContainerClassName="pb-6"
      ListEmptyComponent={
        <Text className="px-6 py-12 text-center text-sm text-muted-foreground">
          No agents in this workspace yet.
        </Text>
      }
      refreshing={isRefetching}
      onRefresh={refetch}
      renderItem={({ item }) => (
        <AgentRow agent={item} runtimeNames={runtimeNames} />
      )}
    />
  );
}

function AgentRow({
  agent,
  runtimeNames,
}: {
  agent: Agent;
  runtimeNames: Map<string, string>;
}) {
  // Absent means "fixed" — never let a missing field read as Auto.
  const isAuto = agent.runtime_routing === "auto";
  const runtimeName = runtimeNames.get(agent.runtime_id);

  return (
    <View className="flex-row items-center gap-3 px-4 py-3">
      <ActorAvatar
        type="agent"
        id={agent.id}
        name={agent.name}
        avatarUrl={agent.avatar_url}
        size={28}
        showPresence
      />
      <View className="flex-1 min-w-0 gap-0.5">
        <Text className="text-sm text-foreground" numberOfLines={1}>
          {agent.name}
        </Text>
        <Text className="text-xs text-muted-foreground" numberOfLines={1}>
          {isAuto
            ? // In auto, runtime_id is the preferred fallback, not a pin —
              // saying "via <name>" would misdescribe it.
              runtimeName
              ? `Auto · prefers ${runtimeName}`
              : "Auto"
            : (runtimeName ?? "No runtime")}
        </Text>
      </View>
      <View className="rounded bg-secondary px-1.5 py-0.5">
        <Text className="text-xs text-muted-foreground">
          {isAuto ? "Auto" : "Fixed"}
        </Text>
      </View>
    </View>
  );
}
