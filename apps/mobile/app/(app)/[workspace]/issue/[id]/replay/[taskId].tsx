/**
 * Run replay — read-only mobile counterpart of web's replay scrubber (k70).
 *
 * Presented as a `modal` Stack screen (see workspace `_layout.tsx`): it is a
 * content view with its own back stack, since a link chip pushes the linked
 * run's replay on top. No resume / steer on mobile — every mutation on a
 * replay targets the machine the daemon runs on.
 *
 * Parity points with web: events are the server's hash-chained log in `seq`
 * order; "so far" counts are cumulative and inclusive (`replayCountsSoFar`);
 * the seal badge trusts the server's `sealed.verified` only; cost uses the
 * same 1e-10 USD tick format as postmortems (`formatPostmortemCost`).
 *
 * Scrubber: `@react-native-community/slider` is not a mobile dependency, so
 * the position moves with Previous / Next buttons and an "N / total" counter.
 */
import { useEffect, useMemo, useState } from "react";
import { ActivityIndicator, Pressable, ScrollView, View } from "react-native";
import { router, useLocalSearchParams } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { taskReplayOptions } from "@/data/queries/task-replay";
import type { RunReplayEvent, RunReplayLink } from "@/data/schemas";
import { useWorkspaceStore } from "@/data/workspace-store";
import { formatPostmortemCost } from "@/lib/postmortem-display";
import {
  formatReplayTokens,
  previewJson,
  replayCountsSoFar,
  replayKindLabel,
  replaySealLabel,
  replaySealState,
} from "@/lib/run-replay-display";
import { timeAgo } from "@/lib/time-ago";

export default function RunReplayRoute() {
  const { id, taskId } = useLocalSearchParams<{ id: string; taskId: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { data, isLoading, error, refetch } = useQuery(
    taskReplayOptions(wsId, taskId ?? ""),
  );

  const events = useMemo(() => data?.events ?? [], [data]);
  const [index, setIndex] = useState(0);
  // Land on the latest event once the log arrives; a growing live run keeps
  // the user's position rather than yanking them to the tail.
  useEffect(() => {
    setIndex((i) => (i === 0 && events.length > 0 ? events.length - 1 : i));
  }, [events.length]);

  if (isLoading) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator />
      </View>
    );
  }

  if (error || !data) {
    return (
      <View className="flex-1 bg-background px-4 gap-3 pt-4">
        <Text className="text-sm text-destructive">
          Could not load this replay
          {error instanceof Error ? `: ${error.message}` : "."}
        </Text>
        <Button variant="outline" onPress={() => refetch()}>
          <Text>Retry</Text>
        </Button>
      </View>
    );
  }

  const { run, cost, sealed } = data;
  const current: RunReplayEvent | undefined = events[index];
  const counts = replayCountsSoFar(events, index);
  const usd =
    typeof cost.cost_usd_ticks === "number" && cost.cost_usd_ticks > 0
      ? formatPostmortemCost(cost.cost_usd_ticks)
      : null;
  const sealState = replaySealState(sealed);

  const openLinked = (link: RunReplayLink) => {
    if (!wsSlug || !link.task_id) return;
    router.push({
      pathname: "/[workspace]/issue/[id]/replay/[taskId]",
      params: { workspace: wsSlug, id, taskId: link.task_id },
    });
  };

  return (
    <ScrollView
      className="flex-1 bg-background"
      contentContainerClassName="p-4 gap-5 pb-10"
    >
      {/* Header */}
      <View className="gap-1">
        <Text className="text-base font-semibold text-foreground">
          {run.agent_name || "Agent"}
        </Text>
        <Text className="text-xs text-muted-foreground">
          {[
            run.status,
            `${events.length} events`,
            formatReplayTokens(cost.input_tokens, cost.output_tokens),
            usd,
          ]
            .filter(Boolean)
            .join(" · ")}
        </Text>
        <View className="flex-row">
          <Text
            className={`text-xs font-medium px-2 py-0.5 rounded-md bg-secondary ${
              sealState === "broken" ? "text-destructive" : "text-foreground"
            }`}
          >
            {replaySealLabel(sealed)}
          </Text>
        </View>
      </View>

      {/* Scrubber */}
      <View className="flex-row items-center justify-between">
        <Button
          variant="outline"
          size="sm"
          disabled={index <= 0}
          onPress={() => setIndex((i) => Math.max(0, i - 1))}
        >
          <Text>Previous</Text>
        </Button>
        <Text className="text-sm text-muted-foreground">
          {events.length === 0 ? "0 / 0" : `${index + 1} / ${events.length}`}
        </Text>
        <Button
          variant="outline"
          size="sm"
          disabled={index >= events.length - 1}
          onPress={() => setIndex((i) => Math.min(events.length - 1, i + 1))}
        >
          <Text>Next</Text>
        </Button>
      </View>

      {/* Current event */}
      {current ? (
        <EventCard event={current} />
      ) : (
        <Text className="text-sm text-muted-foreground">No events yet.</Text>
      )}

      {/* So far */}
      <Text className="text-xs text-muted-foreground">
        So far: {counts.toolCalls} tool calls · {counts.effects} effects ·{" "}
        {counts.decisions} decisions · {counts.steers} steers
      </Text>

      {/* Links */}
      {run.links && run.links.length > 0 ? (
        <View className="gap-2">
          <Text className="text-[11px] font-medium text-muted-foreground uppercase tracking-wide">
            Linked runs
          </Text>
          <View className="flex-row flex-wrap gap-2">
            {run.links.map((link) => (
              <Pressable
                key={`${link.relation}:${link.task_id}`}
                onPress={() => openLinked(link)}
                className="px-3 py-1.5 rounded-full bg-secondary active:opacity-70"
              >
                <Text className="text-xs text-foreground">
                  {link.relation} · {link.agent_name || "Agent"}
                </Text>
              </Pressable>
            ))}
          </View>
        </View>
      ) : null}
    </ScrollView>
  );
}

function EventCard({ event }: { event: RunReplayEvent }) {
  const preview = previewJson(event.data);
  const actor = event.actor.name || event.actor.type;
  return (
    <View className="gap-2 p-3 rounded-lg bg-secondary">
      <View className="flex-row items-center gap-2">
        <Text
          className={`text-xs font-medium px-2 py-0.5 rounded-md bg-background ${
            event.kind === "error" ? "text-destructive" : "text-foreground"
          }`}
        >
          {replayKindLabel(event.kind)}
        </Text>
        <Text className="text-xs text-muted-foreground" numberOfLines={1}>
          {[actor, timeAgo(event.at)].filter(Boolean).join(" · ")}
        </Text>
      </View>
      {event.title ? (
        <Text className="text-sm font-medium text-foreground">{event.title}</Text>
      ) : null}
      {event.text ? (
        <Text className="text-sm text-foreground">{event.text}</Text>
      ) : null}
      {preview ? (
        <Text className="text-xs font-mono text-muted-foreground">{preview}</Text>
      ) : null}
    </View>
  );
}
