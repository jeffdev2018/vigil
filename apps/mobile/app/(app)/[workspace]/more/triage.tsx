/**
 * Triage queue — mobile parity with web `packages/views/triage/components/
 * triage-page.tsx`.
 *
 * Same product semantics:
 *   - the same four state buckets, in the same order (`TRIAGE_STATES`);
 *   - the same keyset pagination (`next_cursor`), so the N a user counts on
 *     mobile is the N web shows for the same state;
 *   - the same stats line (pending, oldest waiting, dropped in 24h).
 *
 * UI diverges where the phone requires it: web is a three-pane page (stats +
 * list + detail aside) — mobile keeps the stats header and the list here and
 * pushes the detail as its own screen (`triage/[id]`), because a 375pt-wide
 * device has no room for a side-by-side reading pane.
 *
 * Deliberately NOT ported: multi-select batch accept (a hover-revealed
 * checkbox column with no phone equivalent) and the auto-ML suggestion panel
 * (`GET /api/triage/suggestions`). Both are additive on top of this screen.
 */
import { useMemo, useState } from "react";
import { ActivityIndicator, FlatList, Pressable, View } from "react-native";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { router } from "expo-router";
import type { TriageItem, TriageItemState } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import {
  flattenTriagePages,
  triageItemsOptions,
  triageStatsOptions,
} from "@/data/queries/triage";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  TRIAGE_STATES,
  TRIAGE_STATE_LABEL,
  ageSecondsToIso,
  triageEmptyMessage,
} from "@/lib/triage-display";
import { timeAgo } from "@/lib/time-ago";

export default function TriagePage() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const [state, setState] = useState<TriageItemState>("pending");

  const { data: stats } = useQuery(triageStatsOptions(wsId));
  const {
    data,
    isLoading,
    error,
    refetch,
    isRefetching,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useInfiniteQuery(triageItemsOptions(wsId, state));

  const items = useMemo(() => flattenTriagePages(data?.pages), [data?.pages]);

  return (
    <View className="flex-1 bg-background">
      <StatsLine
        pending={stats?.pending ?? 0}
        oldestAgeSeconds={stats?.oldest_pending_age_seconds ?? 0}
        dropped24h={stats?.dropped_24h ?? 0}
      />
      <StateTabs state={state} onChange={setState} />
      {isLoading ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : error ? (
        <View className="px-4 gap-3 pt-4">
          <Text className="text-sm text-destructive">
            Could not load the triage queue:{" "}
            {error instanceof Error ? error.message : "unknown error"}
          </Text>
          <Button variant="outline" onPress={() => refetch()}>
            <Text>Retry</Text>
          </Button>
        </View>
      ) : (
        <FlatList
          data={items}
          keyExtractor={(item) => item.id}
          ItemSeparatorComponent={() => (
            <View className="h-px bg-border ml-4" />
          )}
          contentContainerClassName="pb-6"
          ListEmptyComponent={
            <Text className="px-6 py-12 text-center text-sm text-muted-foreground">
              {triageEmptyMessage(state)}
            </Text>
          }
          ListFooterComponent={
            isFetchingNextPage ? (
              <View className="py-4">
                <ActivityIndicator />
              </View>
            ) : null
          }
          // Keyset pagination: same page boundaries as web's "Load more".
          onEndReachedThreshold={0.4}
          onEndReached={() => {
            if (hasNextPage && !isFetchingNextPage) fetchNextPage();
          }}
          refreshing={isRefetching}
          onRefresh={refetch}
          renderItem={({ item }) => (
            <TriageRow
              item={item}
              onPress={() => {
                if (wsSlug) router.push(`/${wsSlug}/triage/${item.id}`);
              }}
            />
          )}
        />
      )}
    </View>
  );
}

/**
 * Mirrors web's TriageStatsBar: pending count, oldest waiting, dropped in 24h.
 * `oldest` is only rendered when something is actually pending — matching
 * web's `pending > 0 && oldestAgeSeconds > 0` guard, so an empty queue never
 * shows a stale age.
 */
function StatsLine({
  pending,
  oldestAgeSeconds,
  dropped24h,
}: {
  pending: number;
  oldestAgeSeconds: number;
  dropped24h: number;
}) {
  const parts: string[] = [
    pending > 0 ? `${pending} pending` : "Queue is clear",
  ];
  if (pending > 0 && oldestAgeSeconds > 0) {
    parts.push(`Oldest waiting ${timeAgo(ageSecondsToIso(oldestAgeSeconds))}`);
  }
  if (dropped24h > 0) parts.push(`${dropped24h} dropped in 24h`);
  return (
    <Text className="px-4 pt-3 text-xs text-muted-foreground">
      {parts.join(" · ")}
    </Text>
  );
}

/** Same pill row shape as the scope tabs on Issues / My Issues. */
function StateTabs({
  state,
  onChange,
}: {
  state: TriageItemState;
  onChange: (next: TriageItemState) => void;
}) {
  return (
    <View className="flex-row items-center gap-1 px-4 pt-2 pb-2">
      {TRIAGE_STATES.map((s) => {
        const active = s === state;
        return (
          <Button
            key={s}
            variant="outline"
            size="sm"
            onPress={() => onChange(s)}
            className={active ? "bg-accent" : ""}
            accessibilityState={{ selected: active }}
          >
            <Text
              numberOfLines={1}
              className={
                active ? "text-accent-foreground" : "text-muted-foreground"
              }
            >
              {TRIAGE_STATE_LABEL[s]}
            </Text>
          </Button>
        );
      })}
    </View>
  );
}

/**
 * One queue entry. Second line mirrors web's row: source name + age. The
 * collapse badge (`×N`, "this delivery was seen N times") is on the right so
 * it does not push the title.
 */
function TriageRow({
  item,
  onPress,
}: {
  item: TriageItem;
  onPress: () => void;
}) {
  return (
    <Pressable
      onPress={onPress}
      className="flex-row items-center gap-3 px-4 py-3 active:bg-secondary"
      accessibilityLabel={item.title}
    >
      <View className="flex-1 min-w-0 gap-0.5">
        <Text className="text-sm text-foreground" numberOfLines={2}>
          {item.title}
        </Text>
        <Text className="text-xs text-muted-foreground" numberOfLines={1}>
          {[item.source_name, timeAgo(item.first_seen_at)]
            .filter(Boolean)
            .join(" · ")}
        </Text>
      </View>
      {item.collapse_count > 1 ? (
        <View className="rounded bg-secondary px-1.5 py-0.5">
          <Text className="text-xs text-muted-foreground">
            ×{item.collapse_count}
          </Text>
        </View>
      ) : null}
    </Pressable>
  );
}
