/**
 * "Automate on a schedule" (JEF-373) — turn a sentence into an autopilot,
 * without leaving the issue.
 *
 * Not to be confused with "Make it recurring" on the issue itself
 * (components/issue/recurrence-section.tsx), which files THIS issue again on
 * a schedule. This sheet creates an autopilot: a standing job that RUNS.
 *
 * Mobile has no autopilots surface (managing them stays on web/desktop), so
 * this is the phone's only door to "every Monday at 9, send me the open
 * tickets". It is reached from the Schedule-follow-up sheet, and it always
 * proposes with `issue_id`, which means the result is a Decision Card on
 * THIS issue — the autopilot is created paused and never runs until someone
 * answers that card (server/internal/handler/autopilot_draft.go).
 *
 * Two steps, because propose writes: draft first (writes nothing, shows the
 * schedule the model read and its next three runs), then create. A 503 means
 * no model is configured — nothing to retry, so the sheet says where to go
 * instead.
 */
import { useMemo, useState } from "react";
import { ActionSheetIOS, Alert, Pressable, ScrollView, View } from "react-native";
import { router, useLocalSearchParams } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import type { AutopilotDraft } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { AutosizeTextArea } from "@/components/ui/autosize-textarea";
import { MOBILE_PLACEHOLDER_COLOR } from "@/components/ui/input-tokens";
import { issueDetailOptions } from "@/data/queries/issues";
import { agentListOptions } from "@/data/queries/agents";
import {
  useDraftAutopilot,
  useProposeAutopilot,
} from "@/data/mutations/autopilots";
import { useWorkspaceStore } from "@/data/workspace-store";

import { autopilotDraftError, formatNextRuns } from "@/lib/followup-display";
import { cn } from "@/lib/utils";

/** Mirrors `autopilotDraftMaxText` in the handler. */
const MAX_TEXT = 2000;

const DEVICE_TZ = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";

export default function ProposeAutopilotSheet() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: issue } = useQuery(issueDetailOptions(wsId, id));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const draftMutation = useDraftAutopilot();
  const propose = useProposeAutopilot(id);

  const [text, setText] = useState("");
  const [draft, setDraft] = useState<AutopilotDraft | null>(null);
  const [error, setError] = useState<string | null>(null);

  const liveAgents = useMemo(
    () => agents.filter((a) => !a.archived_at),
    [agents],
  );
  const issueAgentId =
    issue?.assignee_type === "agent" && issue?.assignee_id
      ? issue.assignee_id
      : null;
  const [pickedAgentId, setPickedAgentId] = useState<string | null>(null);
  const assigneeId = pickedAgentId ?? issueAgentId ?? liveAgents[0]?.id ?? null;
  const assigneeName = liveAgents.find((a) => a.id === assigneeId)?.name ?? null;

  const busy = draftMutation.isPending || propose.isPending;
  const canDraft = text.trim() !== "" && text.length <= MAX_TEXT && !busy;

  const runDraft = () => {
    if (!canDraft) return;
    setError(null);
    draftMutation.mutate(
      { text: text.trim(), timezone: DEVICE_TZ },
      {
        onSuccess: (res) => setDraft(res.draft),
        onError: (e) => setError(autopilotDraftError(e)),
      },
    );
  };

  const create = () => {
    if (!draft || !assigneeId || busy) return;
    setError(null);
    propose.mutate(
      {
        title: draft.title,
        cron_expression: draft.cron_expression,
        timezone: draft.timezone,
        description: draft.description,
        execution_mode: draft.execution_mode,
        issue_title_template: draft.issue_title_template || undefined,
        assignee_id: assigneeId,
      },
      {
        onSuccess: () => {
          Alert.alert(
            "Proposed",
            "The autopilot is paused. Answer the decision card on this issue to activate it.",
          );
          router.back();
        },
        onError: (e) => setError(autopilotDraftError(e)),
      },
    );
  };

  const pickAgent = () => {
    if (liveAgents.length === 0) return;
    ActionSheetIOS.showActionSheetWithOptions(
      {
        title: "Which agent should run it?",
        options: [...liveAgents.map((a) => a.name), "Cancel"],
        cancelButtonIndex: liveAgents.length,
      },
      (index) => {
        const agent = liveAgents[index];
        if (agent) setPickedAgentId(agent.id);
      },
    );
  };

  return (
    <ScrollView
      className="flex-1"
      contentContainerClassName="pb-8"
      stickyHeaderIndices={[0]}
      keyboardShouldPersistTaps="handled"
    >
      <View className="flex-row items-center justify-between bg-background px-4 pb-2 pt-4">
        <View className="flex-1 min-w-0 pr-2">
          <Text className="text-base font-semibold text-foreground">
            Automate on a schedule
          </Text>
          <Text className="text-xs text-muted-foreground">
            Creates an autopilot from a sentence
          </Text>
        </View>
        <Pressable
          onPress={draft ? create : runDraft}
          disabled={draft ? !assigneeId || busy : !canDraft}
          hitSlop={6}
          accessibilityRole="button"
          accessibilityLabel={draft ? "Create the autopilot paused" : "Read the sentence"}
          className={cn(
            "rounded-md px-3 py-1.5",
            (draft ? !assigneeId || busy : !canDraft)
              ? "opacity-50"
              : "active:bg-secondary",
          )}
        >
          <Text className="text-sm font-semibold text-primary">
            {propose.isPending
              ? "Creating…"
              : draftMutation.isPending
                ? "Reading…"
                : draft
                  ? "Create paused"
                  : "Preview"}
          </Text>
        </Pressable>
      </View>

      <View className="gap-4 px-4 pt-2">
        <View className="gap-1">
          <Text className="text-xs text-muted-foreground">In one sentence</Text>
          <AutosizeTextArea
            value={text}
            onChangeText={(next) => {
              setText(next);
              // The preview belongs to the sentence that produced it.
              if (draft) setDraft(null);
            }}
            placeholder="Every Monday at 9, list the tickets still open"
            placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
            minHeight={80}
            maxHeight={200}
            editable={!busy}
            autoFocus
            accessibilityLabel="Describe the recurring work"
          />
          {text.length > MAX_TEXT ? (
            <Text className="text-xs text-destructive">
              {`At most ${MAX_TEXT} characters.`}
            </Text>
          ) : null}
        </View>

        {draft ? (
          <View className="gap-2 rounded-md border border-border p-3">
            <Text className="text-sm font-medium text-foreground">
              {draft.title}
            </Text>
            <Text className="text-xs text-muted-foreground">
              {`${draft.cron_expression} · ${draft.timezone}`}
            </Text>
            {!!draft.reason && (
              <Text className="text-sm text-muted-foreground">{draft.reason}</Text>
            )}
            <Text className="text-xs text-muted-foreground">
              {draft.execution_mode === "create_issue"
                ? "Creates an issue at each run"
                : "Runs without creating an issue"}
            </Text>
            {formatNextRuns(draft.next_runs).length > 0 ? (
              <View className="gap-0.5">
                <Text className="text-xs uppercase tracking-wider text-muted-foreground">
                  Next runs
                </Text>
                {formatNextRuns(draft.next_runs).map((run) => (
                  <Text key={run} className="text-sm text-muted-foreground">
                    • {run}
                  </Text>
                ))}
              </View>
            ) : null}
          </View>
        ) : null}

        {draft ? (
          <Pressable
            onPress={pickAgent}
            disabled={liveAgents.length === 0 || busy}
            accessibilityLabel="Pick the agent that runs it"
            className="flex-row items-center justify-between rounded-md border border-border px-3 py-2.5 active:bg-secondary"
          >
            <Text className="text-sm text-muted-foreground">Runs as</Text>
            <Text className="text-sm text-foreground">
              {assigneeName ?? "No agent in this workspace"}
            </Text>
          </Pressable>
        ) : null}

        {draft ? (
          <Text className="text-xs text-muted-foreground">
            It is created paused. A decision card lands on this issue —
            activating it there is what makes it run.
          </Text>
        ) : null}

        {error ? <Text className="text-xs text-destructive">{error}</Text> : null}
      </View>
    </ScrollView>
  );
}
