import { useState } from "react";
import { Alert, FlatList, Pressable, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { router } from "expo-router";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { inboxDecisionsOptions } from "@/data/queries/inbox";
import { useRespondInboxDecision } from "@/data/mutations/inbox";
import type { InboxDecision } from "@/data/schemas";
import { useWorkspaceStore } from "@/data/workspace-store";

/**
 * Inbox zero (K63): the Decision Cards waiting for me, answered in one tap.
 * Parity with web (packages/views/inbox/components/decisions-view.tsx):
 * same server projection (risk then deadline, five cards plus the total),
 * same options and recommended option, same respond endpoint (K01), the
 * card leaves the list on the refetch. Mobile differs in interaction only:
 * the free-text answer goes through Alert.prompt instead of an inline form.
 */
export default function InboxDecisions() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { data, isLoading, error, refetch } = useQuery(inboxDecisionsOptions(wsId));
  const respond = useRespondInboxDecision();
  const [answered, setAnswered] = useState<Record<string, boolean>>({});

  const answer = (item: InboxDecision, a: { option_id?: string; modified_text?: string }) =>
    respond.mutate(
      { issueId: item.issue_id, decisionId: item.decision.id, answer: a },
      {
        onSuccess: () => setAnswered((prev) => ({ ...prev, [item.decision.id]: true })),
        onError: (e) => Alert.alert("Could not send the answer", e instanceof Error ? e.message : "unknown error"),
      },
    );
  const askOther = (item: InboxDecision) =>
    Alert.prompt("Something else", "Describe what to do instead", (text) => {
      if (text && text.trim()) answer(item, { modified_text: text.trim() });
    });

  if (isLoading) {
    return (
      <View className="flex-1 bg-background gap-3 p-4">
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-24 w-full" />
      </View>
    );
  }
  if (error || !data) {
    return (
      <View className="flex-1 bg-background gap-3 p-4">
        <Text className="text-sm text-destructive">The decisions could not be loaded.</Text>
        <Button variant="outline" onPress={() => refetch()}>
          <Text>Retry</Text>
        </Button>
      </View>
    );
  }
  const remaining = data.total - data.decisions.length;
  return (
    <View className="flex-1 bg-background">
      <FlatList
        data={data.decisions}
        keyExtractor={(item) => item.decision.id}
        contentContainerClassName="gap-3 p-4 pb-8"
        ListEmptyComponent={
          <Text className="py-12 text-center text-sm text-muted-foreground">Inbox zero: no decision is waiting for you.</Text>
        }
        ListFooterComponent={
          remaining > 0 ? (
            <Text className="pt-2 text-center text-sm text-muted-foreground">{remaining} more waiting after these</Text>
          ) : null
        }
        renderItem={({ item }) => {
          const d = item.decision;
          const done = answered[d.id] === true;
          return (
            <Card className="gap-2 p-3">
              <Pressable
                onPress={() =>
                  router.push({
                    pathname: "/[workspace]/issue/[id]",
                    params: { workspace: wsSlug ?? "", id: item.issue_id },
                  })
                }
              >
                <View className="flex-row items-center gap-2">
                  <Text className="text-xs font-mono text-muted-foreground">{item.issue_identifier || item.issue_id.slice(0, 8)}</Text>
                  <Text className="flex-1 text-xs text-muted-foreground" numberOfLines={1}>{item.issue_title}</Text>
                  {d.urgency === "high" && <Text className="text-xs font-medium text-warning">urgent</Text>}
                </View>
              </Pressable>
              <Text className="text-base font-medium">{d.question}</Text>
              {done ? (
                <Text className="text-sm text-success">Answered</Text>
              ) : (
                <View className="gap-2">
                  {d.options.map((o) => (
                    <Button
                      key={o.id}
                      variant={o.id === d.recommended_option_id ? "default" : "outline"}
                      disabled={respond.isPending}
                      onPress={() => answer(item, { option_id: o.id })}
                    >
                      <Text>{o.id === d.recommended_option_id ? `${o.label} · recommended` : o.label}</Text>
                    </Button>
                  ))}
                  <Button variant="ghost" disabled={respond.isPending} onPress={() => askOther(item)}>
                    <Text>Answer with something else…</Text>
                  </Button>
                </View>
              )}
            </Card>
          );
        }}
      />
    </View>
  );
}
