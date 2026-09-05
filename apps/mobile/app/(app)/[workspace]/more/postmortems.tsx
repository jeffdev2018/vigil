/**
 * Postmortems list — mobile parity with web
 * `packages/views/postmortem/components/postmortem-page.tsx`.
 *
 * Same three state buckets in the same order, the same per-state counts from
 * `GET /api/postmortems/stats`, and the same keyset pagination, so the number
 * a user sees on the badge matches web's sidebar badge for the same
 * workspace.
 *
 * Row fields mirror web's list item: the summary (two lines), the
 * `failure_reason` as a mono chip, and the age.
 */
import { useMemo, useState } from "react";
import { ActivityIndicator, FlatList, Pressable, View } from "react-native";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { router } from "expo-router";
import type { Postmortem, PostmortemState } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import {
  flattenPostmortemPages,
  postmortemItemsOptions,
  postmortemStatsOptions,
} from "@/data/queries/postmortem";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  POSTMORTEM_STATES,
  POSTMORTEM_STATE_LABEL,
  formatPostmortemCost,
  postmortemEmptyMessage,
} from "@/lib/postmortem-display";
import { timeAgo } from "@/lib/time-ago";

export default function PostmortemsPage() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const [state, setState] = useState<PostmortemState>("draft");

  const { data: stats } = useQuery(postmortemStatsOptions(wsId));
  const {
    data,
    isLoading,
    error,
    refetch,
    isRefetching,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useInfiniteQuery(postmortemItemsOptions(wsId, state));

  const items = useMemo(
    () => flattenPostmortemPages(data?.pages),
    [data?.pages],
  );

  return (
    <View className="flex-1 bg-background">
      {/* Per-state counts on the pills, like web's filter chips. */}
      <View className="flex-row items-center gap-1 px-4 pt-3 pb-2">
        {POSTMORTEM_STATES.map((s) => {
          const active = s === state;
          const count = stats?.[s] ?? 0;
          return (
            <Button
              key={s}
              variant="outline"
              size="sm"
              onPress={() => setState(s)}
              className={active ? "bg-accent" : ""}
              accessibilityState={{ selected: active }}
            >
              <Text
                numberOfLines={1}
                className={
                  active ? "text-accent-foreground" : "text-muted-foreground"
                }
              >
                {count > 0
                  ? `${POSTMORTEM_STATE_LABEL[s]} ${count}`
                  : POSTMORTEM_STATE_LABEL[s]}
              </Text>
            </Button>
          );
        })}
      </View>

      {isLoading ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : error ? (
        <View className="px-4 gap-3 pt-4">
          <Text className="text-sm text-destructive">
            Could not load postmortems:{" "}
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
              {postmortemEmptyMessage(state)}
            </Text>
          }
          ListFooterComponent={
            isFetchingNextPage ? (
              <View className="py-4">
                <ActivityIndicator />
              </View>
            ) : null
          }
          onEndReachedThreshold={0.4}
          onEndReached={() => {
            if (hasNextPage && !isFetchingNextPage) fetchNextPage();
          }}
          refreshing={isRefetching}
          onRefresh={refetch}
          renderItem={({ item }) => (
            <PostmortemRow
              item={item}
              onPress={() => {
                if (wsSlug) router.push(`/${wsSlug}/postmortem/${item.id}`);
              }}
            />
          )}
        />
      )}
    </View>
  );
}

function PostmortemRow({
  item,
  onPress,
}: {
  item: Postmortem;
  onPress: () => void;
}) {
  const cost =
    typeof item.cost_usd_ticks === "number" && item.cost_usd_ticks > 0
      ? formatPostmortemCost(item.cost_usd_ticks)
      : null;
  return (
    <Pressable
      onPress={onPress}
      className="gap-1 px-4 py-3 active:bg-secondary"
      accessibilityLabel={item.summary || item.failure_reason}
    >
      <Text className="text-sm text-foreground" numberOfLines={2}>
        {item.summary || item.failure_reason || "Postmortem"}
      </Text>
      <View className="flex-row items-center gap-1.5">
        {item.failure_reason ? (
          <View className="rounded bg-secondary px-1.5 py-0.5">
            <Text className="font-mono text-xs text-muted-foreground">
              {item.failure_reason}
            </Text>
          </View>
        ) : null}
        <Text className="text-xs text-muted-foreground">
          {[timeAgo(item.created_at), cost].filter(Boolean).join(" · ")}
        </Text>
      </View>
    </Pressable>
  );
}
