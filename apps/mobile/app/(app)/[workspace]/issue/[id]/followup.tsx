/**
 * "Schedule follow-up" (JEF-373) — pick when the issue's agent should wake
 * up, with what note.
 *
 * A formSheet route: a form with a keyboard (apps/mobile/CLAUDE.md Lesson 5
 * container table), self-contained per Lesson 5 rule 3 — it reads the issue
 * out of the TanStack Query cache, calls its own mutation and `router.back()`s
 * rather than handing a callback up to the issue screen.
 *
 * Three one-tap choices cover almost every real follow-up ("in 1 hour",
 * "tomorrow 9:00", "next Monday 9:00"); "Custom" hands the rest to the
 * native UIDatePicker (iOS-native rung of the component waterfall). The agent
 * row only appears when the issue is NOT assigned to an agent, which is
 * exactly when the server requires `agent_id`
 * (server/internal/handler/followups.go CreateIssueFollowup).
 *
 * Awaits the server before dismissing: scheduling is a navigate-away flow,
 * and the 429 budget refusal has to be readable in place.
 */
import { useMemo, useState } from "react";
import { ActionSheetIOS, Pressable, ScrollView, View } from "react-native";
import DateTimePicker from "@react-native-community/datetimepicker";
import { router, useLocalSearchParams } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { AutosizeTextArea } from "@/components/ui/autosize-textarea";
import { MOBILE_PLACEHOLDER_COLOR } from "@/components/ui/input-tokens";
import { issueDetailOptions } from "@/data/queries/issues";
import { issueFollowupsOptions } from "@/data/queries/followups";
import { agentListOptions } from "@/data/queries/agents";
import { useScheduleFollowup } from "@/data/mutations/followups";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  FOLLOWUP_NOTE_MAX,
  followupNoteError,
  followupQuickChoices,
  followupScheduleError,
  followupWhenError,
  toRfc3339,
  type FollowupQuickChoiceId,
} from "@/lib/followup-display";
import { cn } from "@/lib/utils";

type Selection = FollowupQuickChoiceId | "custom";

export default function ScheduleFollowupSheet() {
  const { id, workspace } = useLocalSearchParams<{
    id: string;
    workspace: string;
  }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: issue } = useQuery(issueDetailOptions(wsId, id));
  const { data: followups } = useQuery(issueFollowupsOptions(wsId, id));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const schedule = useScheduleFollowup(id);

  // `now` is frozen for the life of the sheet so the quick choices don't
  // drift under the user's finger between render and submit.
  const now = useMemo(() => new Date(), []);
  const choices = useMemo(() => followupQuickChoices(now), [now]);

  const [selection, setSelection] = useState<Selection>("tomorrow_9");
  const [customAt, setCustomAt] = useState<Date>(
    () => new Date(now.getTime() + 3_600_000),
  );
  const [note, setNote] = useState("");
  const [serverError, setServerError] = useState<string | null>(null);

  const issueAgentId =
    issue?.assignee_type === "agent" && issue?.assignee_id
      ? issue.assignee_id
      : null;
  const liveAgents = agents.filter((a) => !a.archived_at);
  const [pickedAgentId, setPickedAgentId] = useState<string | null>(null);
  const agentId = issueAgentId ?? pickedAgentId;
  const agentName =
    liveAgents.find((a) => a.id === agentId)?.name ??
    (issueAgentId ? "the issue's agent" : null);

  const at =
    selection === "custom"
      ? customAt
      : (choices.find((c) => c.id === selection)?.at ?? customAt);

  const whenError = followupWhenError(at, new Date());
  const noteError = followupNoteError(note);
  const agentError =
    agentId === null
      ? liveAgents.length === 0
        ? "This workspace has no agent to wake up."
        : "Pick the agent that should wake up."
      : null;
  const blocker = whenError ?? noteError ?? agentError;

  const submit = () => {
    if (blocker || schedule.isPending) return;
    setServerError(null);
    schedule.mutate(
      {
        when: toRfc3339(at),
        note: note.trim() || undefined,
        // Omitted when the issue already carries an agent assignee — the
        // server resolves it, and sending a stale id would be worse.
        agent_id: issueAgentId ? undefined : (agentId ?? undefined),
      },
      {
        onSuccess: () => router.back(),
        onError: (e) => setServerError(followupScheduleError(e)),
      },
    );
  };

  const pickAgent = () => {
    if (liveAgents.length === 0) return;
    ActionSheetIOS.showActionSheetWithOptions(
      {
        title: "Which agent should wake up?",
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
    <View className="flex-1">
      <View className="flex-row items-center justify-between px-4 pb-2 pt-4">
        <Text className="text-base font-semibold text-foreground">
          Schedule follow-up
        </Text>
        <Pressable
          onPress={submit}
          disabled={!!blocker || schedule.isPending}
          hitSlop={6}
          accessibilityRole="button"
          accessibilityLabel="Schedule this follow-up"
          className={cn(
            "rounded-md px-3 py-1.5",
            blocker || schedule.isPending ? "opacity-50" : "active:bg-secondary",
          )}
        >
          <Text className="text-sm font-semibold text-primary">
            {schedule.isPending ? "Scheduling…" : "Schedule"}
          </Text>
        </Pressable>
      </View>

      <ScrollView
        className="flex-1"
        contentContainerClassName="gap-4 px-4 pb-8 pt-2"
        keyboardShouldPersistTaps="handled"
      >
        <View className="flex-row flex-wrap gap-2">
          {choices.map((choice) => (
            <Chip
              key={choice.id}
              label={choice.label}
              selected={selection === choice.id}
              onPress={() => setSelection(choice.id)}
            />
          ))}
          <Chip
            label="Custom…"
            selected={selection === "custom"}
            onPress={() => setSelection("custom")}
          />
        </View>

        {selection === "custom" ? (
          <View className="items-start">
            <DateTimePicker
              value={customAt}
              mode="datetime"
              display="compact"
              minimumDate={now}
              onChange={(_event, selected) => {
                if (selected) setCustomAt(selected);
              }}
            />
          </View>
        ) : null}

        {issueAgentId === null ? (
          <Pressable
            onPress={pickAgent}
            disabled={liveAgents.length === 0}
            accessibilityLabel="Pick the agent to wake up"
            className="flex-row items-center justify-between rounded-md border border-border px-3 py-2.5 active:bg-secondary"
          >
            <Text className="text-sm text-muted-foreground">Agent</Text>
            <Text className="text-sm text-foreground">
              {agentName ?? "Choose…"}
            </Text>
          </Pressable>
        ) : null}

        <View className="gap-1">
          <Text className="text-xs text-muted-foreground">Note</Text>
          <AutosizeTextArea
            value={note}
            onChangeText={setNote}
            placeholder="What should the agent pick up when it wakes?"
            placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
            minHeight={80}
            maxHeight={200}
            accessibilityLabel="Follow-up note"
          />
          {noteError ? (
            <Text className="text-xs text-destructive">{noteError}</Text>
          ) : (
            <Text className="text-xs text-muted-foreground">
              {`Optional · ${FOLLOWUP_NOTE_MAX - note.length} characters left`}
            </Text>
          )}
        </View>

        {whenError ? (
          <Text className="text-xs text-destructive">{whenError}</Text>
        ) : null}
        {agentError ? (
          <Text className="text-xs text-destructive">{agentError}</Text>
        ) : null}
        {/* The 429 budget refusal names which ceiling was hit (per agent or
            per workspace) — shown verbatim, in place, because retrying the
            same request is not the answer. */}
        {serverError ? (
          <Text className="text-xs text-destructive">{serverError}</Text>
        ) : null}

        {/* Only quote a ceiling the server actually sent — the shared schema
            degrades a missing/malformed budget to 0, which must not read as
            "you have none left". */}
        {(followups?.budget.max_per_agent_per_day ?? 0) > 0 ? (
          <Text className="text-xs text-muted-foreground">
            {`Up to ${followups?.budget.max_per_agent_per_day} follow-ups per agent per day.`}
          </Text>
        ) : null}

        {/* Mobile has no autopilots screen; this is the one door to
            "every Monday at 9, send me the open tickets" on a phone. NOT the
            same thing as "Make it recurring" on the issue itself, which files
            this issue again (components/issue/recurrence-section.tsx) — this
            one builds an autopilot that RUNS. `replace` rather than `push` so
            the flow stays one sheet deep instead of stacking a formSheet on
            a formSheet. */}
        <Pressable
          onPress={() =>
            router.replace({
              pathname: "/[workspace]/issue/[id]/autopilot",
              params: { workspace, id },
            })
          }
          accessibilityLabel="Automate this on a schedule"
          className="self-start pt-2"
        >
          <Text className="text-sm text-primary">Automate on a schedule…</Text>
        </Pressable>
      </ScrollView>
    </View>
  );
}

function Chip({
  label,
  selected,
  onPress,
}: {
  label: string;
  selected: boolean;
  onPress: () => void;
}) {
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityState={{ selected }}
      className={cn(
        "rounded-full border px-3 py-1.5",
        // Selected reads on border + text weight, which hover/press does not
        // touch — per the "active state stays identifiable" UI rule.
        selected
          ? "border-primary bg-primary/10"
          : "border-border active:bg-secondary",
      )}
    >
      <Text
        className={cn(
          "text-sm",
          selected ? "font-semibold text-primary" : "text-foreground",
        )}
      >
        {label}
      </Text>
    </Pressable>
  );
}
