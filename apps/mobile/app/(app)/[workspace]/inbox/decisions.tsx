import { useState } from "react";
import { ActionSheetIOS, Alert, FlatList, Pressable, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { Stack, router } from "expo-router";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { IconButton } from "@/components/ui/icon-button";
import { Skeleton } from "@/components/ui/skeleton";
import { SwipeableDecisionCard } from "@/components/inbox/swipeable-decision-card";
import { inboxDecisionsOptions } from "@/data/queries/inbox";
import { useRespondInboxDecision } from "@/data/mutations/inbox";
import type { InboxDecision } from "@/data/schemas";
import { useWorkspaceStore } from "@/data/workspace-store";
import { cn } from "@/lib/utils";
import {
  condensedSummary,
  swipeRightAnswer,
  swipeRightLabel,
  type DecisionAnswer,
} from "@/lib/decision-swipe";

/**
 * Inbox zero (K63) + gesture review (K36): the Decision Cards waiting for me,
 * answered with a swipe from a condensed summary.
 *
 * Parity with web (packages/views/inbox/components/decisions-view.tsx): same
 * server projection (risk then deadline, five cards plus the total), same
 * options and recommended option, same respond endpoint (K01), the card
 * leaves the list on the refetch. `lib/decision-swipe.ts` is the canonical
 * place for what a swipe answers and how the summary reads — see its tests.
 *
 * Mobile differs in interaction only:
 *   - swipe right reveals "Answer <recommended>", the same body tapping
 *     web's highlighted button sends;
 *   - swipe left reveals "Options", which opens the native iOS action sheet
 *     (mobile CLAUDE.md Lesson 5: one-of-N from a server-driven short list);
 *   - free text goes through `Alert.prompt` instead of web's inline form.
 *
 * Nothing is optimistic: the mutation awaits the server and a failure returns
 * the card with the reason plus a retry, which is also what a swipe made
 * offline shows. There is no replay queue — an unsent answer is not held.
 * The list itself still renders offline off the React Query cache (60s
 * staleTime / 10min gcTime from `data/query-client.ts`).
 */
export default function InboxDecisions() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { data, isLoading, error, refetch } = useQuery(inboxDecisionsOptions(wsId));
  const respond = useRespondInboxDecision();
  const [answered, setAnswered] = useState<Record<string, boolean>>({});
  // Per-card failure text. The card comes back carrying it; retry clears it.
  const [failed, setFailed] = useState<Record<string, string>>({});

  const answer = (item: InboxDecision, a: DecisionAnswer) => {
    setFailed(({ [item.decision.id]: _cleared, ...rest }) => rest);
    respond.mutate(
      { issueId: item.issue_id, decisionId: item.decision.id, answer: a },
      {
        onSuccess: () => setAnswered((prev) => ({ ...prev, [item.decision.id]: true })),
        onError: (e) =>
          setFailed((prev) => ({
            ...prev,
            [item.decision.id]:
              e instanceof Error && e.message ? e.message : "The answer could not be sent",
          })),
      },
    );
  };

  const askOther = (item: InboxDecision) =>
    Alert.prompt("Something else", "Describe what to do instead", (text) => {
      if (text && text.trim()) answer(item, { modified_text: text.trim() });
    });

  // Native one-of-N sheet: every option, then free text, then cancel. It
  // never answers on its own — the user still picks a row.
  const openOptions = (item: InboxDecision) => {
    const options = item.decision.options ?? [];
    const labels = options.map((o) =>
      o.id === item.decision.recommended_option_id ? `${o.label} · recommended` : o.label,
    );
    ActionSheetIOS.showActionSheetWithOptions(
      {
        title: item.decision.question,
        options: [...labels, "Answer with something else…", "Cancel"],
        cancelButtonIndex: labels.length + 1,
      },
      (index) => {
        if (index < labels.length) {
          const chosen = options[index];
          if (chosen?.id) answer(item, { option_id: chosen.id });
          return;
        }
        if (index === labels.length) askOther(item);
      },
    );
  };

  const headerRight = () => (
    <IconButton
      name="mic-outline"
      accessibilityLabel="Dictate an issue"
      onPress={() => wsSlug && router.push(`/${wsSlug}/new-issue-voice`)}
    />
  );

  if (isLoading) {
    return (
      <View className="flex-1 bg-background gap-3 p-4">
        <Stack.Screen options={{ headerRight }} />
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-24 w-full" />
      </View>
    );
  }
  if (error || !data) {
    return (
      <View className="flex-1 bg-background gap-3 p-4">
        <Stack.Screen options={{ headerRight }} />
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
      <Stack.Screen options={{ headerRight }} />
      <FlatList
        data={data.decisions}
        keyExtractor={(item) => item.decision.id}
        contentContainerClassName="gap-3 p-4 pb-8"
        ListEmptyComponent={
          <Text className="py-12 text-center text-sm text-muted-foreground">
            Everything is handled
          </Text>
        }
        ListFooterComponent={
          remaining > 0 ? (
            <Text className="pt-2 text-center text-sm text-muted-foreground">
              {remaining} more waiting after these
            </Text>
          ) : null
        }
        renderItem={({ item }) => (
          <DecisionRow
            item={item}
            done={answered[item.decision.id] === true}
            failure={failed[item.decision.id]}
            pending={respond.isPending}
            onOpenIssue={() =>
              router.push({
                pathname: "/[workspace]/issue/[id]",
                params: { workspace: wsSlug ?? "", id: item.issue_id },
              })
            }
            onAnswer={(a) => answer(item, a)}
            onOptions={() => openOptions(item)}
          />
        )}
      />
    </View>
  );
}

function DecisionRow({
  item,
  done,
  failure,
  pending,
  onOpenIssue,
  onAnswer,
  onOptions,
}: {
  item: InboxDecision;
  done: boolean;
  failure: string | undefined;
  pending: boolean;
  onOpenIssue: () => void;
  onAnswer: (answer: DecisionAnswer) => void;
  onOptions: () => void;
}) {
  const d = item.decision;
  const summary = condensedSummary(d);
  const swipeAnswer = swipeRightAnswer(d);
  const card = (
    <Card className="gap-2 p-3">
      <Pressable onPress={onOpenIssue}>
        <View className="flex-row items-center gap-2">
          <Text className="text-xs font-mono text-muted-foreground">
            {item.issue_identifier || item.issue_id.slice(0, 8)}
          </Text>
          <Text className="flex-1 text-xs text-muted-foreground" numberOfLines={1}>
            {item.issue_title}
          </Text>
          <Text
            className={cn(
              "text-xs",
              d.urgency === "high" ? "font-medium text-warning" : "text-muted-foreground",
            )}
          >
            {summary.urgencyLabel}
          </Text>
        </View>
      </Pressable>
      <Text className="text-base font-medium">{d.question}</Text>
      {summary.deadlineText ? (
        <Text className="text-xs text-muted-foreground">{summary.deadlineText}</Text>
      ) : null}
      {done ? (
        <Text className="text-sm text-success">Answered</Text>
      ) : (
        <>
          {summary.optionLines.map((line, i) => (
            <Text key={i} className="text-sm text-muted-foreground" numberOfLines={2}>
              {line}
            </Text>
          ))}
          {failure ? (
            <View className="gap-2">
              <Text className="text-sm text-destructive">{failure}</Text>
              <Button
                variant="outline"
                disabled={pending}
                onPress={() => (swipeAnswer ? onAnswer(swipeAnswer) : onOptions())}
              >
                <Text>Retry</Text>
              </Button>
            </View>
          ) : (
            <Text className="text-xs text-muted-foreground">
              Swipe right to answer · swipe left for options
            </Text>
          )}
        </>
      )}
    </Card>
  );

  // An answered card is inert until the refetch drops it.
  if (done) return card;
  return (
    <SwipeableDecisionCard
      answerLabel={swipeAnswer ? swipeRightLabel(d) : null}
      onAnswer={() => swipeAnswer && onAnswer(swipeAnswer)}
      onOptions={onOptions}
      disabled={pending}
    >
      {card}
    </SwipeableDecisionCard>
  );
}
