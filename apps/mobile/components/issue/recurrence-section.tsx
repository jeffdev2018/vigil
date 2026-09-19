/**
 * "Recurrence" (OS plan, table stakes) — the standing order that spawns this
 * issue again: "every Monday at 09:00", or "when the current one is closed".
 *
 * Sits next to FollowupsSection because both answer "what happens next on
 * this issue without anyone typing" — a follow-up wakes the agent once, a
 * recurrence files the issue again.
 *
 * Mirrors the backend contract directly — server/internal/handler/
 * issue_recurrence.go — the same way FollowupsSection mirrors followups.go.
 *
 * The rule belongs to a SERIES, not to one issue: opening any occurrence
 * answers with the same rule, and the source is marked in the list so the
 * user can tell which issue the rule was filed on. When the issue on screen
 * is an occurrence rather than the source, the block says so.
 *
 * Permissions: the server lets any member set, read and clear a rule and
 * refuses a task token with 403, so there is no client-side role gate to
 * mirror — the 403 is printed by the sheet if it ever happens.
 */
import { Alert, Pressable, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useQuery } from "@tanstack/react-query";
import { router } from "expo-router";
import type {
  Issue,
  IssueRecurrenceOccurrence,
  IssueStatusCategory,
} from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { StatusIcon } from "@/components/ui/status-icon";
import { issueRecurrenceOptions } from "@/data/queries/recurrence";
import {
  useClearIssueRecurrence,
  useSetIssueRecurrence,
} from "@/data/mutations/recurrence";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useIssueStatuses } from "@/lib/use-issue-statuses";
import {
  formatRecurrenceRuns,
  recurrenceSentence,
  recurrenceSetError,
} from "@/lib/recurrence-display";
import { followupAbsoluteTime } from "@/lib/followup-display";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

export function RecurrenceSection({ issue }: { issue: Issue }) {
  const issueId = issue.id;
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { data, isPending } = useQuery(issueRecurrenceOptions(wsId, issueId));
  const setRecurrence = useSetIssueRecurrence(issueId);
  const clear = useClearIssueRecurrence(issueId);
  const catalog = useIssueStatuses();
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;

  const openSheet = () => {
    if (!wsSlug) return;
    router.push({
      pathname: "/[workspace]/issue/[id]/recurrence",
      params: { workspace: wsSlug, id: issueId },
    });
  };

  const openIssue = (occurrenceId: string) => {
    if (!wsSlug || occurrenceId === issueId) return;
    router.push({
      pathname: "/[workspace]/issue/[id]",
      params: { workspace: wsSlug, id: occurrenceId },
    });
  };

  // A first load must not flash "Make it recurring" at an issue that does
  // recur — the CTA would be a lie for one frame and a mis-tap for the user.
  if (isPending) return null;

  const header = (
    <View className="flex-row items-center gap-2">
      <Ionicons name="repeat-outline" size={14} color={mutedFg} />
      <Text className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
        Recurrence
      </Text>
    </View>
  );

  if (!data) {
    return (
      <View className="px-4 pt-4 pb-2 border-t border-border gap-2">
        {header}
        <Text className="text-sm text-muted-foreground">
          File this issue again on a schedule, or each time the current one is
          closed.
        </Text>
        <Button
          variant="outline"
          size="sm"
          className="self-start"
          onPress={openSheet}
        >
          <Ionicons name="repeat-outline" size={14} color={mutedFg} />
          <Text>Make it recurring</Text>
        </Button>
      </View>
    );
  }

  const { recurrence, source, occurrences, next_runs } = data;
  const isOccurrence = !!source.id && source.id !== issueId;
  const runs = formatRecurrenceRuns(next_runs);

  const toggleEnabled = (enabled: boolean) => {
    setRecurrence.mutate(
      {
        cron_expression: recurrence.cron_expression,
        timezone: recurrence.timezone,
        mode: recurrence.mode,
        enabled,
      },
      {
        onError: (e) =>
          Alert.alert("Could not change the recurrence", recurrenceSetError(e)),
      },
    );
  };

  const confirmStop = () => {
    Alert.alert(
      "Stop this recurrence?",
      `${source.identifier || "This issue"} will not be filed again. Issues it already created stay as ordinary issues.`,
      [
        { text: "Keep it", style: "cancel" },
        {
          text: "Stop recurring",
          style: "destructive",
          onPress: () =>
            clear.mutate(undefined, {
              onError: (e) =>
                Alert.alert("Could not stop it", recurrenceSetError(e)),
            }),
        },
      ],
    );
  };

  const busy = setRecurrence.isPending || clear.isPending;

  return (
    <View className="px-4 pt-4 pb-2 border-t border-border gap-2">
      {header}

      {isOccurrence ? (
        <Pressable
          onPress={() => openIssue(source.id)}
          accessibilityRole="link"
          accessibilityLabel={`Open the source issue ${source.identifier}`}
          className="self-start rounded-md active:bg-secondary"
        >
          <Text className="text-sm text-primary">
            {`Part of the series ${source.identifier || "this issue belongs to"}`}
          </Text>
        </Pressable>
      ) : null}

      <View className="gap-1 rounded-md border border-border px-3 py-2.5">
        <View className="flex-row items-start gap-3">
          <View className="flex-1 min-w-0 gap-0.5">
            <Text className="text-sm text-foreground">
              {recurrenceSentence(recurrence)}
            </Text>
            <Text className="text-xs text-muted-foreground">
              {recurrence.occurrence_count === 1
                ? "1 issue filed so far"
                : `${recurrence.occurrence_count} issues filed so far`}
            </Text>
          </View>
          {/* Pausing is a toggle whose outcome the server still owns
              (next_run_at is recomputed), so the switch reflects the cache and
              snaps back when the write fails. */}
          <Switch
            checked={recurrence.enabled}
            disabled={busy}
            onCheckedChange={toggleEnabled}
          />
        </View>

        {!recurrence.enabled ? (
          <Text className="text-xs text-muted-foreground">
            Paused — nothing is filed until it is switched back on.
          </Text>
        ) : runs.length > 0 ? (
          <View className="gap-0.5 pt-1">
            <Text className="text-xs uppercase tracking-wider text-muted-foreground">
              Next runs
            </Text>
            {runs.map((run) => (
              <Text key={run} className="text-sm text-muted-foreground">
                {`• ${run}`}
              </Text>
            ))}
          </View>
        ) : null}
      </View>

      <View className="flex-row gap-2">
        <Button variant="outline" size="sm" disabled={busy} onPress={openSheet}>
          <Text>Edit</Text>
        </Button>
        <Pressable
          disabled={busy}
          onPress={confirmStop}
          hitSlop={8}
          accessibilityRole="button"
          accessibilityLabel="Stop this recurrence"
          className="justify-center rounded-md px-2 active:bg-secondary"
        >
          <Text className="text-sm text-destructive">Stop recurring</Text>
        </Pressable>
      </View>

      {occurrences.length > 0 ? (
        <View className="gap-1 pt-1">
          <Text className="text-xs uppercase tracking-wider text-muted-foreground">
            {occurrences.length === 1 ? "The series" : `The series · ${occurrences.length}`}
          </Text>
          {occurrences.map((occurrence) => (
            <OccurrenceRow
              key={occurrence.id}
              occurrence={occurrence}
              isSource={occurrence.id === source.id}
              isCurrent={occurrence.id === issueId}
              statusLabel={catalog.labelOf(occurrence.status)}
              statusCategory={catalog.categoryOf(occurrence.status)}
              statusColor={catalog.colorOf(occurrence.status)}
              onPress={() => openIssue(occurrence.id)}
            />
          ))}
        </View>
      ) : null}
    </View>
  );
}

function OccurrenceRow({
  occurrence,
  isSource,
  isCurrent,
  statusLabel,
  statusCategory,
  statusColor,
  onPress,
}: {
  occurrence: IssueRecurrenceOccurrence;
  isSource: boolean;
  isCurrent: boolean;
  statusLabel: string;
  statusCategory: IssueStatusCategory;
  statusColor: string | null;
  onPress: () => void;
}) {
  return (
    <Pressable
      onPress={onPress}
      disabled={isCurrent}
      accessibilityRole="link"
      accessibilityLabel={`${occurrence.identifier} ${occurrence.title}, ${statusLabel}`}
      className="flex-row items-center gap-2 rounded-md px-1 py-1.5 active:bg-secondary"
    >
      <StatusIcon
        status={occurrence.status}
        category={statusCategory}
        color={statusColor ?? undefined}
        size={14}
      />
      <View className="flex-1 min-w-0">
        <Text
          numberOfLines={1}
          className={
            isCurrent
              ? "text-sm font-semibold text-foreground"
              : "text-sm text-foreground"
          }
        >
          {occurrence.title || occurrence.identifier}
        </Text>
        <Text numberOfLines={1} className="text-xs text-muted-foreground">
          {[
            occurrence.identifier,
            followupAbsoluteTime(occurrence.created_at),
            isSource ? "source" : null,
            isCurrent ? "this issue" : null,
          ]
            .filter(Boolean)
            .join(" · ")}
        </Text>
      </View>
    </Pressable>
  );
}
