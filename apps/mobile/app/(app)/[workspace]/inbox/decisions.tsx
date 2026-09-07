/**
 * Durable decisions screen — mobile counterpart of web InboxDecisions
 * (packages/views/inbox/components/inbox-decisions.tsx).
 *
 * Notifications stay on the Inbox tab; answering never happens by reading or
 * archiving a notification. Pending vs History are segments on this screen
 * (web uses the same two lists; mobile avoids ?view= query params).
 */
import { useMemo, useState } from "react";
import { FlatList, Pressable, View } from "react-native";
import { useInfiniteQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { DecisionCard } from "@/components/inbox/decision-card";
import { inboxDecisionsOptions } from "@/data/queries/decisions";
import { useWorkspaceStore } from "@/data/workspace-store";
import { cn } from "@/lib/utils";

export default function InboxDecisionsScreen() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const [history, setHistory] = useState(false);
  const query = useInfiniteQuery(inboxDecisionsOptions(wsId, history));
  const rows = useMemo(
    () => query.data?.pages.flatMap((page) => page.decisions) ?? [],
    [query.data],
  );

  return (
    <View className="flex-1 bg-background">
      <View className="px-4 pt-3 pb-2 gap-3 border-b border-border">
        <Text className="text-sm text-muted-foreground">
          Questions addressed to you stay here until you respond and, when
          needed, start a follow-up. Reading or archiving a notification never
          answers a decision.
        </Text>
        <View className="flex-row gap-2">
          <Segment
            label="To resolve"
            active={!history}
            onPress={() => setHistory(false)}
          />
          <Segment
            label="History"
            active={history}
            onPress={() => setHistory(true)}
          />
        </View>
      </View>

      {query.isPending ? (
        <View className="px-4 pt-4">
          <Text className="text-sm text-muted-foreground">
            Loading decisions…
          </Text>
        </View>
      ) : query.isError ? (
        <View className="px-4 pt-4 gap-3">
          <Text className="text-sm text-destructive">
            Decisions could not be loaded.
          </Text>
          <Button variant="outline" onPress={() => void query.refetch()}>
            <Text>Try again</Text>
          </Button>
        </View>
      ) : (
        <FlatList
          data={rows}
          keyExtractor={(item) => item.id}
          contentContainerClassName="pt-3 pb-8"
          ListEmptyComponent={
            <View className="mx-4 rounded-lg border border-border p-6">
              <Text className="text-sm text-muted-foreground">
                {history
                  ? "No resolved decisions yet."
                  : "No decisions need your attention."}
              </Text>
            </View>
          }
          renderItem={({ item }) => <DecisionCard decision={item} />}
          ListFooterComponent={
            query.hasNextPage ? (
              <View className="px-4 pt-1">
                <Button
                  variant="outline"
                  disabled={query.isFetchingNextPage}
                  onPress={() => void query.fetchNextPage()}
                >
                  <Text>Load more</Text>
                </Button>
              </View>
            ) : null
          }
        />
      )}
    </View>
  );
}

function Segment({
  label,
  active,
  onPress,
}: {
  label: string;
  active: boolean;
  onPress: () => void;
}) {
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityState={{ selected: active }}
      className={cn(
        "rounded-md px-3 py-2",
        active ? "bg-secondary" : "bg-transparent",
      )}
    >
      <Text
        className={cn(
          "text-sm",
          active ? "font-medium text-foreground" : "text-muted-foreground",
        )}
      >
        {label}
      </Text>
    </Pressable>
  );
}
