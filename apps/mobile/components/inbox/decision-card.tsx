/**
 * One durable decision addressed to this member.
 *
 * Product semantics mirror packages/views/inbox/components/inbox-decisions.tsx
 * DecisionCard: answer / cancel / resume, 409 conflicts, 4000-char cap.
 * UI differs for phone: TextField, Alert confirm, Pressable suggestion chips.
 */
import { useState } from "react";
import { Alert, Pressable, View } from "react-native";
import { router } from "expo-router";
import type { IssueDecision } from "@multica/core/api/schemas";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { TextField } from "@/components/ui/text-field";
import {
  decisionAnswerErrorMessage,
  decisionResumeErrorMessage,
  useAnswerIssueDecision,
  useResumeIssueDecision,
} from "@/data/mutations/decisions";
import { useWorkspaceStore } from "@/data/workspace-store";

const MAX_ANSWER_CHARS = 4000;

export function DecisionCard({ decision }: { decision: IssueDecision }) {
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const answerMutation = useAnswerIssueDecision(decision.issueId, decision.id);
  const resumeMutation = useResumeIssueDecision(decision.issueId, decision.id);
  const [draft, setDraft] = useState("");
  const [error, setError] = useState<string | null>(null);

  const row =
    resumeMutation.data ??
    (decision.status === "open" ? answerMutation.data : undefined) ??
    decision;
  const busy = answerMutation.isPending || resumeMutation.isPending;
  const answer = draft.trim();
  const answerTooLong = [...answer].length > MAX_ANSWER_CHARS;

  const respond = (status: "answered" | "cancelled") => {
    setError(null);
    answerMutation.mutate(
      { status, answer: status === "answered" ? answer : "" },
      {
        onSuccess: () => setDraft(""),
        onError: (err) => setError(decisionAnswerErrorMessage(err)),
      },
    );
  };

  const confirmCancel = () => {
    Alert.alert(
      "Cancel request?",
      "This closes the decision without an answer. The agent will not get a follow-up from this request.",
      [
        { text: "Keep request", style: "cancel" },
        {
          text: "Confirm cancellation",
          style: "destructive",
          onPress: () => respond("cancelled"),
        },
      ],
    );
  };

  const openIssue = () => {
    if (!wsSlug) return;
    router.push(`/(app)/${wsSlug}/issue/${row.issueId}`);
  };

  return (
    <View className="mx-4 mb-3 rounded-lg border border-border bg-card p-4 gap-3">
      <View className="flex-row items-center justify-between gap-2">
        <Pressable onPress={openIssue} accessibilityRole="link">
          <Text className="text-xs text-primary underline">Open issue</Text>
        </Pressable>
        <Text className="text-xs text-muted-foreground shrink-0">
          {new Date(row.createdAt).toLocaleString()}
        </Text>
      </View>

      <Text className="text-sm font-medium text-foreground">{row.question}</Text>
      {row.context ? (
        <Text className="text-sm text-muted-foreground">{row.context}</Text>
      ) : null}

      {row.status === "open" ? (
        <>
          {row.options.length > 0 ? (
            <View className="flex-row flex-wrap gap-2">
              {row.options.map((option) => (
                <Pressable
                  key={option}
                  disabled={busy}
                  onPress={() => setDraft(option)}
                  className="rounded-md border border-border px-3 py-2 active:bg-secondary"
                  accessibilityRole="button"
                  accessibilityLabel={`Use suggestion: ${option}`}
                >
                  <Text className="text-sm text-foreground">{option}</Text>
                </Pressable>
              ))}
            </View>
          ) : null}

          <View className="gap-1.5">
            <Text className="text-xs font-medium text-foreground">
              Your answer
            </Text>
            <TextField
              value={draft}
              editable={!busy}
              onChangeText={setDraft}
              placeholder="Choose an option or write your answer…"
              multiline
              className="min-h-[88px] h-auto py-2"
              style={{ textAlignVertical: "top" }}
            />
            <Text className="text-xs text-muted-foreground">
              Your answer is final once saved. Saving it does not start a run.
              Maximum 4,000 characters.
            </Text>
          </View>

          <View className="flex-row flex-wrap gap-2">
            <Button
              size="sm"
              disabled={busy || !answer || answerTooLong}
              onPress={() => respond("answered")}
            >
              <Text>Save answer</Text>
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={busy}
              onPress={confirmCancel}
            >
              <Text>Cancel request</Text>
            </Button>
          </View>
        </>
      ) : null}

      {row.status === "answered" ? (
        <View className="gap-2">
          <Text className="text-xs font-medium text-foreground">
            Answer saved
          </Text>
          <Text className="text-sm text-foreground">{row.answer}</Text>
          {row.resumeTaskId ? (
            <Text className="text-xs text-muted-foreground">
              Follow-up run: {row.resumeTaskId}
            </Text>
          ) : (
            <>
              <Text className="text-xs text-muted-foreground">
                Start a new run with this answer once the source run has
                finished. Existing tool approvals still apply.
              </Text>
              <Button
                size="sm"
                disabled={busy}
                onPress={() => {
                  setError(null);
                  resumeMutation.mutate(undefined, {
                    onError: (err) =>
                      setError(decisionResumeErrorMessage(err)),
                  });
                }}
              >
                <Text>Start follow-up</Text>
              </Button>
            </>
          )}
        </View>
      ) : null}

      {row.status === "cancelled" ? (
        <Text className="text-sm text-muted-foreground">
          This request was cancelled.
        </Text>
      ) : null}

      {error ? (
        <Text className="text-sm text-destructive" accessibilityRole="alert">
          {error}
        </Text>
      ) : null}
    </View>
  );
}
