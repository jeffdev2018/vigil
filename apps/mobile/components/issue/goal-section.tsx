/**
 * "Goal" (goal_loop) — the standing goal an agent works an issue toward
 * across bounded continuations, and the state of that loop: status,
 * continuation count, why the last run wasn't enough, the next step, the
 * evidence gathered, and a pending question from the agent to the team.
 *
 * No web reference exists yet (goal loop is being built for web in the same
 * PR wave — see apps/mobile/data/queries/issue-goal.ts); this section
 * mirrors the backend contract directly (server/pkg/goalstate/goalstate.go,
 * server/internal/handler/issue_goal.go) rather than a web component.
 *
 * Hidden entirely when the issue has no goal AND isn't assigned to an agent
 * (nothing to show, nothing to set). When assigned to an agent with no goal
 * yet, a compact "Set a goal" affordance opens the form; when a goal
 * exists, "Edit" opens the same form pre-filled. `assignee_type === "agent"`
 * gates authoring — squads/members don't run the goal loop.
 */
import { useState } from "react";
import { Alert, Pressable, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useQuery } from "@tanstack/react-query";
import type { Issue } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { TextField } from "@/components/ui/text-field";
import { AutosizeTextArea } from "@/components/ui/autosize-textarea";
import { issueGoalOptions } from "@/data/queries/issue-goal";
import {
  usePauseIssueGoal,
  useResumeIssueGoal,
  useAnswerIssueGoal,
  useSetIssueGoal,
} from "@/data/mutations/issue-goal";
import { ApiError } from "@/data/api";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  apiErrorMessage,
  issueGoalEvidencePreview,
  issueGoalFormError,
  issueGoalBlockerLabel,
  issueGoalOutcomeLabel,
  issueGoalProgressLabel,
  issueGoalStatusLabel,
  ISSUE_GOAL_MAX_CONTINUATIONS_MAX,
  ISSUE_GOAL_MAX_CONTINUATIONS_MIN,
} from "@/lib/issue-goal-display";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

const DEFAULT_MAX_CONTINUATIONS = 8;

function onMutationError(title: string) {
  return (e: unknown) => Alert.alert(title, apiErrorMessage(e, "Please try again."));
}

export function GoalSection({ issue }: { issue: Issue }) {
  const issueId = issue.id;
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: goal } = useQuery(issueGoalOptions(wsId, issueId));
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;

  const pause = usePauseIssueGoal(issueId);
  const resume = useResumeIssueGoal(issueId);
  const answer = useAnswerIssueGoal(issueId);
  const setGoal = useSetIssueGoal(issueId);

  const [showAllEvidence, setShowAllEvidence] = useState(false);
  const [draftAnswer, setDraftAnswer] = useState("");
  const [formOpen, setFormOpen] = useState(false);
  const [formGoal, setFormGoal] = useState("");
  const [formMax, setFormMax] = useState(DEFAULT_MAX_CONTINUATIONS);

  const canAuthor = issue.assignee_type === "agent";
  if (!goal && !canAuthor) return null;

  const busy =
    pause.isPending || resume.isPending || answer.isPending || setGoal.isPending;

  const openCreate = () => {
    setFormGoal("");
    setFormMax(DEFAULT_MAX_CONTINUATIONS);
    setFormOpen(true);
  };
  const openEdit = () => {
    if (!goal) return;
    setFormGoal(goal.goal);
    setFormMax(goal.max_continuations || DEFAULT_MAX_CONTINUATIONS);
    setFormOpen(true);
  };
  const formError = issueGoalFormError(formGoal, formMax);
  const submitForm = () => {
    if (formError) return;
    setGoal.mutate(
      { goal: formGoal.trim(), max_continuations: formMax },
      {
        onSuccess: () => setFormOpen(false),
        onError: (e) =>
          Alert.alert("Couldn't save the goal", apiErrorMessage(e, "Please try again.")),
      },
    );
  };

  if (formOpen) {
    return (
      <View className="px-4 pt-4 pb-2 border-t border-border gap-2">
        <View className="flex-row items-center gap-2">
          <Ionicons name="flag-outline" size={14} color={mutedFg} />
          <Text className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
            {goal ? "Edit goal" : "Set a goal"}
          </Text>
        </View>
        <AutosizeTextArea
          value={formGoal}
          onChangeText={setFormGoal}
          placeholder="Definition of done for this issue…"
          multiline
          editable={!setGoal.isPending}
        />
        <View className="flex-row items-center gap-2">
          <Pressable
            disabled={setGoal.isPending}
            onPress={() =>
              setFormMax((m) => Math.max(ISSUE_GOAL_MAX_CONTINUATIONS_MIN, m - 1))
            }
            hitSlop={8}
            className="h-8 w-8 items-center justify-center rounded-md border border-border"
          >
            <Ionicons name="remove" size={16} color={mutedFg} />
          </Pressable>
          <TextField
            value={String(formMax)}
            onChangeText={(t) => setFormMax(parseInt(t, 10) || 0)}
            keyboardType="number-pad"
            editable={!setGoal.isPending}
            className="w-14 text-center"
          />
          <Pressable
            disabled={setGoal.isPending}
            onPress={() =>
              setFormMax((m) => Math.min(ISSUE_GOAL_MAX_CONTINUATIONS_MAX, m + 1))
            }
            hitSlop={8}
            className="h-8 w-8 items-center justify-center rounded-md border border-border"
          >
            <Ionicons name="add" size={16} color={mutedFg} />
          </Pressable>
          <Text className="text-xs text-muted-foreground">max continuations</Text>
        </View>
        {formError && <Text className="text-xs text-destructive">{formError}</Text>}
        <View className="flex-row gap-2 mt-1">
          <Button
            size="sm"
            disabled={setGoal.isPending || !!formError}
            onPress={submitForm}
          >
            <Text>Save</Text>
          </Button>
          <Button
            variant="ghost"
            size="sm"
            disabled={setGoal.isPending}
            onPress={() => setFormOpen(false)}
          >
            <Text>Cancel</Text>
          </Button>
        </View>
      </View>
    );
  }

  if (!goal) {
    return (
      <View className="px-4 pt-4 pb-2 border-t border-border">
        <Button variant="outline" size="sm" onPress={openCreate} className="self-start">
          <Ionicons name="flag-outline" size={14} color={mutedFg} />
          <Text>Set a goal</Text>
        </Button>
      </View>
    );
  }

  const outcome = issueGoalOutcomeLabel(goal.last_outcome);
  const evidence = issueGoalEvidencePreview(goal, 3);
  const shownEvidence = showAllEvidence ? goal.evidence : evidence.shown;
  const question = goal.question;
  const isWaiting = goal.status === "waiting_user" && question && !question.answer;

  const submitAnswer = (value: string) => {
    const trimmed = value.trim();
    if (!trimmed) return;
    answer.mutate(trimmed, {
      onSuccess: () => setDraftAnswer(""),
      onError: (e) => {
        if (e instanceof ApiError && e.status === 409) {
          Alert.alert("Already answered", "Nothing is waiting for an answer anymore.");
          return;
        }
        onMutationError("Couldn't send the answer")(e);
      },
    });
  };

  return (
    <View className="px-4 pt-4 pb-2 border-t border-border gap-2">
      <View className="flex-row items-center gap-2">
        <Ionicons name="flag-outline" size={14} color={mutedFg} />
        <Text className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
          Goal
        </Text>
        <View className="rounded bg-muted px-1.5 py-0.5">
          <Text className="text-xs text-muted-foreground">
            {issueGoalStatusLabel(goal.status)}
          </Text>
        </View>
        {canAuthor && (
          <Pressable onPress={openEdit} className="ml-auto">
            <Text className="text-xs text-primary">Edit</Text>
          </Pressable>
        )}
      </View>

      {!!goal.goal && <Text className="text-sm">{goal.goal}</Text>}

      <Text className="text-xs text-muted-foreground">
        {issueGoalProgressLabel(goal)}
      </Text>

      {(goal.last_blocker || goal.last_reason) && (
        <Text className="text-sm text-muted-foreground">
          {goal.last_blocker ? `${issueGoalBlockerLabel(goal.last_blocker)}: ` : ""}
          {goal.last_reason}
        </Text>
      )}

      {outcome && (
        <Text className="text-sm text-muted-foreground">{outcome}</Text>
      )}

      {!!goal.next_step && (
        <View className="flex-row gap-1.5">
          <Text className="text-sm font-medium">Next:</Text>
          <Text className="text-sm flex-1">{goal.next_step}</Text>
        </View>
      )}

      {goal.evidence.length > 0 && (
        <View className="gap-1">
          {shownEvidence.map((line, i) => (
            <Text key={i} className="text-sm text-muted-foreground">
              • {line}
            </Text>
          ))}
          {evidence.hiddenCount > 0 && (
            <Pressable onPress={() => setShowAllEvidence((v) => !v)}>
              <Text className="text-sm text-primary">
                {showAllEvidence
                  ? "Show less"
                  : `Show all (${goal.evidence.length})`}
              </Text>
            </Pressable>
          )}
        </View>
      )}

      {question && (
        <View className="gap-2 mt-1 rounded-md border border-border p-3">
          <Text className="text-sm font-medium">{question.prompt}</Text>
          {question.answer ? (
            <Text className="text-sm text-muted-foreground">
              Answered{question.answered_by_name ? ` by ${question.answered_by_name}` : ""}: {question.answer}
            </Text>
          ) : isWaiting && question.kind === "choice" && question.options.length > 0 ? (
            <View className="gap-2">
              {question.options.map((opt) => (
                <Button
                  key={opt}
                  variant="outline"
                  size="sm"
                  disabled={busy}
                  onPress={() => submitAnswer(opt)}
                >
                  <Text>{opt}</Text>
                </Button>
              ))}
            </View>
          ) : isWaiting ? (
            <View className="gap-2">
              <TextField
                value={draftAnswer}
                onChangeText={setDraftAnswer}
                placeholder="Your answer"
                editable={!busy}
              />
              <Button
                size="sm"
                disabled={busy || draftAnswer.trim().length === 0}
                onPress={() => submitAnswer(draftAnswer)}
              >
                <Text>Answer</Text>
              </Button>
            </View>
          ) : null}
        </View>
      )}

      <View className="flex-row gap-2 mt-1">
        {goal.status === "active" && (
          <Button
            variant="outline"
            size="sm"
            disabled={busy}
            onPress={() => pause.mutate(undefined, { onError: onMutationError("Couldn't pause") })}
          >
            <Text>Pause</Text>
          </Button>
        )}
        {goal.status === "paused" && (
          <Button
            variant="outline"
            size="sm"
            disabled={busy}
            onPress={() => resume.mutate(undefined, { onError: onMutationError("Couldn't resume") })}
          >
            <Text>Resume</Text>
          </Button>
        )}
      </View>
    </View>
  );
}
