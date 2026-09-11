/**
 * ApprovalAskCard: one pending ask, decidable in place. Mirrors
 * `packages/views/approvals/approval-card.tsx` (`ApprovalCard`) —
 * controls per source, gate details fold, countdown, cannot-decide reason
 * — over RN primitives. Mounted in the issue timeline
 * (components/issue/timeline-list.tsx) and the inbox detail screen
 * (app/(app)/[workspace]/inbox/[id].tsx).
 *
 * Interaction differs from web per the iOS-native > RNR > discuss waterfall
 * (apps/mobile/CLAUDE.md): a decision's free-text answer goes through
 * `Alert.prompt` (mirrors `decisions.tsx` askOther) instead of web's inline
 * textarea toggle; everything else is an inline row of buttons / a
 * TextField, same as web's compact layout.
 *
 * Nothing here is optimistic: the server decides, the feed refreshes on
 * settle (every mutate call invalidates `approvalKeys.all(wsId)`, same as
 * web's `refresh()`).
 */
import { useEffect, useState } from "react";
import { Alert, Pressable, View } from "react-native";
import type { TextStyle } from "react-native";
import { router } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import { useQueryClient } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { TextField } from "@/components/ui/text-field";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { ApiError } from "@/data/api";
import type { ApprovalItem } from "@/data/schemas";
import { approvalKeys } from "@/data/queries/approvals";
import { useRespondInboxDecision } from "@/data/mutations/inbox";
import { useDecideIssueTransitionApproval } from "@/data/mutations/approvals";
import { useAnswerIssueGoal } from "@/data/mutations/issue-goal";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  approvalKindLabel,
  approvalSecondsLeft,
  approvalShortLabel,
  formatCountdown,
  gateDetails,
} from "@/lib/approvals-display";
import { apiErrorMessage } from "@/lib/issue-goal-display";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import { cn } from "@/lib/utils";

// `className="tabular-nums"` compiles to `font-variant-numeric`, which
// react-native-css-interop's property allow-list drops silently — see
// components/chat/status-pill.tsx for the reference explanation.
// `fontVariant` via style is the working equivalent.
const TABULAR_NUMS: TextStyle = { fontVariant: ["tabular-nums"] };

export function ApprovalAskCard({
  approval,
  wsId,
  showIssue = false,
}: {
  approval: ApprovalItem;
  wsId: string;
  showIssue?: boolean;
}) {
  const qc = useQueryClient();
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;

  const respond = useRespondInboxDecision();
  const decideTransition = useDecideIssueTransitionApproval(approval.issue.id);
  const answerGoal = useAnswerIssueGoal(approval.issue.id);

  const [note, setNote] = useState("");
  const [goalAnswer, setGoalAnswer] = useState("");
  const [secondsLeft, setSecondsLeft] = useState(() =>
    approvalSecondsLeft(approval),
  );

  useEffect(() => {
    setSecondsLeft(approvalSecondsLeft(approval));
    if (approvalSecondsLeft(approval) === null) return;
    const id = setInterval(
      () => setSecondsLeft(approvalSecondsLeft(approval)),
      30_000,
    );
    return () => clearInterval(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [approval.expires_at, approval.sla_deadline_at]);

  const busy =
    respond.isPending || decideTransition.isPending || answerGoal.isPending;
  const refresh = () =>
    qc.invalidateQueries({ queryKey: approvalKeys.all(wsId) });
  const fail = (e: unknown) =>
    Alert.alert("Could not record the answer", apiErrorMessage(e, "Please try again."));

  const answerDecision = (body: { option_id?: string; modified_text?: string }) =>
    respond.mutate(
      { issueId: approval.issue.id, decisionId: approval.id, answer: body },
      { onError: fail, onSettled: refresh },
    );
  const answerTransition = (decision: "approve" | "reject") =>
    decideTransition.mutate(
      { requestId: approval.id, decision, note: note.trim() || undefined },
      { onError: fail, onSettled: refresh },
    );
  const answerQuestion = (answer: string) =>
    answerGoal.mutate(answer, {
      onSuccess: () => setGoalAnswer(""),
      onError: (e: unknown) => {
        // 409 = the question was already answered / superseded elsewhere.
        if (e instanceof ApiError && e.status === 409) {
          Alert.alert("Already answered", "This question is no longer waiting for an answer.");
          return;
        }
        fail(e);
      },
      onSettled: refresh,
    });

  // Mirrors decisions.tsx `askOther` — same title/message, same endpoint.
  const answerDifferently = () =>
    Alert.prompt("Something else", "Describe what to do instead", (text) => {
      if (text && text.trim()) answerDecision({ modified_text: text.trim() });
    });

  const gate = gateDetails(approval.gate);
  const countdown = formatCountdown(secondsLeft);
  const expired = secondsLeft === 0;
  const openIssue = () => {
    if (!wsSlug || !approval.issue.id) return;
    router.push({
      pathname: "/[workspace]/issue/[id]",
      params: { workspace: wsSlug, id: approval.issue.id },
    });
  };

  return (
    <View
      className={cn(
        "gap-1.5 rounded-md border p-3",
        expired ? "border-border opacity-80" : "border-warning/60 bg-warning/5",
      )}
    >
      <View className="flex-row items-start gap-2">
        <Ionicons
          name="shield-checkmark-outline"
          size={16}
          color={expired ? mutedFg : THEME[colorScheme].warning}
          style={{ marginTop: 1 }}
        />
        <View className="flex-1 gap-0.5">
          <View className="flex-row flex-wrap items-center gap-x-2 gap-y-0.5">
            <Text className="text-sm font-medium">{approvalKindLabel(approval)}</Text>
            {approval.asked_by?.name ? (
              <Text className="text-xs text-muted-foreground">
                asked by {approval.asked_by.name}
              </Text>
            ) : null}
            {approval.urgency === "high" ? (
              <Text className="text-xs font-semibold uppercase text-destructive">
                {approval.urgency}
              </Text>
            ) : null}
          </View>
          {showIssue && approval.issue.identifier ? (
            <Pressable onPress={openIssue} disabled={!wsSlug}>
              <Text className="text-xs text-muted-foreground" numberOfLines={1}>
                {approval.issue.identifier} · {approval.issue.title}
              </Text>
            </Pressable>
          ) : null}
          <Text className="text-sm">{approval.question || approvalShortLabel(approval)}</Text>
          {approval.transition ? (
            <Text className="text-xs text-muted-foreground">
              {approval.transition.from_status} → {approval.transition.to_status}
            </Text>
          ) : null}
        </View>
        {countdown ? (
          <View className="flex-row items-center gap-1">
            <Ionicons
              name="time-outline"
              size={12}
              color={expired ? THEME[colorScheme].destructive : mutedFg}
            />
            <Text
              className={cn(
                "text-xs",
                expired ? "text-destructive" : "text-muted-foreground",
              )}
              style={TABULAR_NUMS}
            >
              {expired ? "Expired" : `${countdown} left`}
            </Text>
          </View>
        ) : null}
      </View>

      {approval.gate ? (
        <Collapsible>
          <CollapsibleTrigger asChild>
            <View
              accessibilityRole="button"
              accessibilityLabel="What it would do"
              className="ml-6 flex-row items-center gap-1 active:opacity-70"
            >
              <Ionicons name="chevron-forward" size={12} color={mutedFg} />
              <Text className="text-xs text-muted-foreground">What it would do</Text>
              {gate.requiredApprovals > 1 ? (
                <Text
                  className="text-xs text-muted-foreground"
                  style={TABULAR_NUMS}
                >
                  · {gate.approvals}/{gate.requiredApprovals} approvals
                </Text>
              ) : null}
            </View>
          </CollapsibleTrigger>
          <CollapsibleContent>
            <View className="ml-6 mt-1 gap-1 rounded bg-muted/40 p-2">
              <Text className="text-xs">
                <Text className="text-muted-foreground">Gate: </Text>
                {approval.gate.gate_type}
                {approval.gate.summary ? ` · ${approval.gate.summary}` : ""}
              </Text>
              {gate.paths.length > 0 ? (
                <Text className="text-xs" selectable>
                  <Text className="text-muted-foreground">Paths: </Text>
                  {gate.paths.join(", ")}
                </Text>
              ) : null}
              {gate.blastRadius ? (
                <Text className="text-xs">
                  <Text className="text-muted-foreground">Blast radius: </Text>
                  {gate.blastRadius}
                </Text>
              ) : null}
              {gate.params != null ? (
                <Text className="rounded bg-background p-1.5 font-mono text-xs" selectable>
                  {JSON.stringify(gate.params, null, 2)}
                </Text>
              ) : null}
            </View>
          </CollapsibleContent>
        </Collapsible>
      ) : null}

      {!approval.can_decide ? (
        <Text className="ml-6 text-xs text-muted-foreground">
          {approval.cannot_decide_reason === "gate_approvers_policy"
            ? "This workspace reserves approval gates to owners and admins."
            : "You are not an approver for this ask."}
        </Text>
      ) : expired ? null : approval.source === "transition" ? (
        <View className="ml-6 gap-1.5">
          <TextField
            value={note}
            onChangeText={setNote}
            placeholder="Note for the requester (optional)"
            editable={!busy}
          />
          <View className="flex-row gap-1.5">
            <Button size="sm" disabled={busy} onPress={() => answerTransition("approve")}>
              <Text>Approve</Text>
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={busy}
              onPress={() => answerTransition("reject")}
            >
              <Text>Reject</Text>
            </Button>
          </View>
        </View>
      ) : approval.source === "goal_question" ? (
        <View className="ml-6">
          {approval.goal_question?.kind === "choice" ? (
            <View className="flex-row flex-wrap gap-1.5">
              {(approval.goal_question.options ?? []).map((opt) => (
                <Button
                  key={opt}
                  size="sm"
                  variant="outline"
                  disabled={busy}
                  onPress={() => answerQuestion(opt)}
                >
                  <Text>{opt}</Text>
                </Button>
              ))}
            </View>
          ) : (
            <View className="flex-row gap-1.5">
              <TextField
                value={goalAnswer}
                onChangeText={setGoalAnswer}
                placeholder="Type your answer…"
                editable={!busy}
                className="flex-1"
              />
              <Button
                size="sm"
                disabled={busy || !goalAnswer.trim()}
                onPress={() => answerQuestion(goalAnswer.trim())}
              >
                <Text>Send</Text>
              </Button>
            </View>
          )}
        </View>
      ) : (
        <View className="ml-6 gap-1.5">
          <View className="flex-row flex-wrap gap-1.5">
            {approval.options.map((o) => {
              const recommended = o.id === approval.recommended_option_id;
              const destructive = o.id === "deny" || o.id === "reject" || o.id === "discard";
              return (
                <Button
                  key={o.id}
                  size="sm"
                  variant={recommended ? "default" : destructive ? "ghost" : "outline"}
                  disabled={busy}
                  onPress={() => answerDecision({ option_id: o.id })}
                >
                  <Text>
                    {o.label}
                    {recommended ? " · recommended" : ""}
                  </Text>
                </Button>
              );
            })}
          </View>
          <Pressable onPress={answerDifferently} disabled={busy}>
            <Text className="text-xs text-muted-foreground">Answer with something else…</Text>
          </Pressable>
        </View>
      )}
    </View>
  );
}

/**
 * PendingApprovalsBar: sits above a list and names how many asks wait.
 * Mirrors web's `PendingApprovalsBar`; renders nothing when empty.
 */
export function PendingApprovalsBar({
  approvals,
  onSelect,
}: {
  approvals: ApprovalItem[];
  onSelect: (id: string) => void;
}) {
  const { colorScheme } = useColorScheme();
  if (approvals.length === 0) return null;
  return (
    <View className="flex-row flex-wrap items-center gap-2 rounded-md border border-warning/60 bg-warning/10 px-3 py-2">
      <Ionicons
        name="shield-checkmark-outline"
        size={14}
        color={THEME[colorScheme].warning}
      />
      <Text className="text-xs font-medium">
        {approvals.length} awaiting a decision
      </Text>
      {approvals.map((a) => (
        <Pressable
          key={a.id}
          onPress={() => onSelect(a.id)}
          className="rounded border border-border px-2 py-1 active:bg-background"
        >
          <Text className="text-xs" numberOfLines={1}>
            {approvalShortLabel(a)}
          </Text>
        </Pressable>
      ))}
    </View>
  );
}
