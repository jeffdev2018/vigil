import { useMemo, useState } from "react";
import { ActionSheetIOS, Alert, FlatList, Pressable, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { Stack, router } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { IconButton } from "@/components/ui/icon-button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { SwipeableDecisionCard } from "@/components/inbox/swipeable-decision-card";
import { inboxDecisionsOptions } from "@/data/queries/inbox";
import { useRespondInboxDecision } from "@/data/mutations/inbox";
import { useWorkspaceApprovals } from "@/data/queries/approvals";
import type { ApprovalGate, InboxDecision } from "@/data/schemas";
import { useWorkspaceStore } from "@/data/workspace-store";
import { cn } from "@/lib/utils";
import { gateDetails } from "@/lib/approvals-display";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
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
  // Cheap enrichment: the unified approvals feed sometimes carries gate
  // details (paths, blast radius, dual-approval progress) for a Decision
  // Card that is really an approval gate. Map by decision id so DecisionRow
  // can show the same fold ApprovalAskCard uses, when the feed has it.
  const { data: approvalsFeed } = useWorkspaceApprovals(wsId);
  const gatesByDecisionId = useMemo(() => {
    const map = new Map<string, ApprovalGate>();
    for (const a of approvalsFeed?.approvals ?? []) {
      if (a.source === "decision" && a.gate) map.set(a.id, a.gate);
    }
    return map;
  }, [approvalsFeed]);
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
            gate={gatesByDecisionId.get(item.decision.id) ?? null}
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
  gate,
  onOpenIssue,
  onAnswer,
  onOptions,
}: {
  item: InboxDecision;
  done: boolean;
  failure: string | undefined;
  pending: boolean;
  /** Gate details for this decision from the unified approvals feed, when
   *  this Decision Card is really an approval gate. Null for an ordinary
   *  decision, or while the feed hasn't loaded yet. */
  gate: ApprovalGate | null;
  onOpenIssue: () => void;
  onAnswer: (answer: DecisionAnswer) => void;
  onOptions: () => void;
}) {
  const d = item.decision;
  const summary = condensedSummary(d);
  const swipeAnswer = swipeRightAnswer(d);
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;
  const details = gate ? gateDetails(gate) : null;
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
      {!done && gate && details ? (
        <Collapsible>
          <CollapsibleTrigger asChild>
            <View
              accessibilityRole="button"
              accessibilityLabel="What it would do"
              className="flex-row items-center gap-1 active:opacity-70"
            >
              <Ionicons name="chevron-forward" size={12} color={mutedFg} />
              <Text className="text-xs text-muted-foreground">What it would do</Text>
              {details.requiredApprovals > 1 ? (
                <Text className="text-xs tabular-nums text-muted-foreground">
                  · {details.approvals}/{details.requiredApprovals} approvals
                </Text>
              ) : null}
            </View>
          </CollapsibleTrigger>
          <CollapsibleContent>
            <View className="mt-1 gap-1 rounded bg-muted/40 p-2">
              <Text className="text-xs">
                <Text className="text-muted-foreground">Gate: </Text>
                {gate.gate_type}
                {gate.summary ? ` · ${gate.summary}` : ""}
              </Text>
              {details.paths.length > 0 ? (
                <Text className="text-xs" selectable>
                  <Text className="text-muted-foreground">Paths: </Text>
                  {details.paths.join(", ")}
                </Text>
              ) : null}
              {details.blastRadius ? (
                <Text className="text-xs">
                  <Text className="text-muted-foreground">Blast radius: </Text>
                  {details.blastRadius}
                </Text>
              ) : null}
            </View>
          </CollapsibleContent>
        </Collapsible>
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
