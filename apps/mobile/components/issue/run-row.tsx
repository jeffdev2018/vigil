/**
 * Single row inside the agent-runs formSheet route
 * (`app/(app)/[workspace]/issue/[id]/runs.tsx`) AND the Runs fleet screen
 * (`app/(app)/[workspace]/more/runs.tsx`, OS plan chantier 4). Same
 * component for active and past tasks — the trailing Cancel button is
 * conditional on an active status, and the status badge / colour swaps
 * based on the AgentTask.status enum.
 *
 * Tapping a running or finished row opens the read-only run replay
 * (`issue/[id]/replay/[taskId]`, k70). Queued / waiting rows have no event
 * log yet: on the fleet screen they open their issue, in the per-issue sheet
 * they stay inert (see `runRowTapTarget`).
 *
 * Fleet-only props (`agentName`, `issueRef`, `costUsdTicks`, `blockedOn`,
 * `onPressBlocker`, `silent`, `onCancel`) are all optional and undefined in the
 * per-issue sheet, which already knows its own issue and agent from
 * context. When `onCancel` is given it replaces the row's own
 * `useCancelTask(issueId)` mutation — the fleet screen cancels through
 * `POST /api/runs/cancel` (works across issues), not the per-issue
 * cache-scoped mutation this file otherwise uses.
 */
import { Alert, Pressable, View } from "react-native";
import { router } from "expo-router";
import type { AgentTask } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { useCancelTask } from "@/data/mutations/issues";
import type { RunBlocker } from "@/data/schemas";
import { useActorLookup } from "@/data/use-actor-name";
import { useWorkspaceStore } from "@/data/workspace-store";
import { blockerLabel, formatRunCost, runRowTapTarget, runSummaryText } from "@/lib/runs-display";
import { runFailureBadgeLabel } from "@/lib/run-failure-badge";
import { timeAgo } from "@/lib/time-ago";

interface Props {
  task: AgentTask;
  issueId: string;
  /** Fleet only: the server already resolved the agent's name, so skip
   *  the per-row `useActorLookup` render. */
  agentName?: string;
  /** Fleet only: shown as an identifier chip above the summary line —
   *  the per-issue sheet doesn't need it, every row there is already the
   *  current issue. */
  issueRef?: { identifier: string } | null;
  /** Fleet only: rendered inline with the status/time row when > 0. */
  costUsdTicks?: number;
  /** Fleet only: rendered as a tappable chip; opens the blocked-on
   *  issue/decision when pressed. */
  blockedOn?: RunBlocker | null;
  onPressBlocker?: () => void;
  /** Fleet only: a running run whose last activity is older than the
   *  fleet's unresponsive threshold (`isRunSilent` in lib/runs-display.ts,
   *  docs/runs.mdx: "flagged as silent"). */
  silent?: boolean;
  /** Fleet only: overrides the row's default per-issue cancel mutation. */
  onCancel?: (taskId: string) => void;
}

// Every status the daemon still owns — matches server
// `runActiveStatuses` in server/internal/handler/runs.go, which is also
// what `POST /api/runs/cancel` treats as cancellable. The previous list
// here (queued/dispatched/running only) hid the Cancel button on a
// paused, deferred or directory-waiting task even in the per-issue sheet.
const ACTIVE_STATUSES: readonly AgentTask["status"][] = [
  "queued",
  "deferred",
  "dispatched",
  "waiting_local_directory",
  "running",
  "paused",
];

export function RunRow({
  task,
  issueId,
  agentName,
  issueRef,
  costUsdTicks,
  blockedOn,
  onPressBlocker,
  silent,
  onCancel,
}: Props) {
  const { getName } = useActorLookup();
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const isActive = ACTIVE_STATUSES.includes(task.status);
  // `issueRef` is only passed by the fleet screen (null when the run has no
  // issue row), so its presence is what tells the two callers apart.
  const rawTarget = runRowTapTarget(task.status, { inFleet: issueRef !== undefined });
  const tapTarget = rawTarget === "issue" && !issueId ? null : rawTarget;
  const canReplay = tapTarget === "replay";
  const onPressRow = () => {
    if (!wsSlug || !tapTarget) return;
    if (tapTarget === "replay") {
      router.push({
        pathname: "/[workspace]/issue/[id]/replay/[taskId]",
        params: { workspace: wsSlug, id: issueId, taskId: task.id },
      });
      return;
    }
    router.push({
      pathname: "/[workspace]/issue/[id]",
      params: { workspace: wsSlug, id: issueId },
    });
  };
  // Mention markdown renders as its label ("@Analyst"), never as raw link syntax.
  const summary = runSummaryText(task);
  // Past tasks use completed_at when present (server fills it for terminal
  // statuses); active tasks fall back to created_at so the user sees how
  // long it's been waiting.
  const timestamp = task.completed_at || task.created_at;
  const name = agentName ?? getName("agent", task.agent_id);
  const cost = costUsdTicks && costUsdTicks > 0 ? formatRunCost(costUsdTicks) : null;
  const blockerText = blockerLabel(blockedOn);

  return (
    <Pressable
      onPress={tapTarget ? onPressRow : undefined}
      // Long-press-to-cancel is a fleet-only affordance (the Runs screen's
      // rows aren't already sitting next to a visible Cancel button the
      // way a picker sheet's are) — only wired when the caller passed
      // `onCancel`. The per-issue sheet keeps its existing tap-the-button
      // path unchanged.
      onLongPress={isActive && onCancel ? () => confirmCancel(task.id, onCancel) : undefined}
      disabled={!tapTarget && !isActive}
      className="flex-row items-start gap-3 py-2 -mx-2 px-2 rounded-lg active:bg-secondary"
    >
      <ActorAvatar type="agent" id={task.agent_id} size={28} showPresence />
      <View className="flex-1 gap-1">
        {issueRef ? (
          <Text
            className="font-mono text-xs text-muted-foreground"
            numberOfLines={1}
          >
            {issueRef.identifier}
          </Text>
        ) : null}
        <Text
          className="text-sm text-foreground"
          numberOfLines={2}
        >
          <Text className="font-medium">{name}</Text>
          <Text className="text-muted-foreground"> · {summary}</Text>
        </Text>
        <View className="flex-row flex-wrap items-center gap-2">
          <StatusBadge task={task} />
          {silent ? <Text className="text-xs text-destructive">Silent</Text> : null}
          <Text className="text-xs text-muted-foreground">
            {[timestamp ? timeAgo(timestamp) : null, cost].filter(Boolean).join(" · ")}
          </Text>
          {canReplay ? (
            <Text className="text-xs text-brand">Replay ›</Text>
          ) : null}
        </View>
        {blockerText ? (
          <Pressable
            onPress={onPressBlocker}
            disabled={!onPressBlocker}
            className="self-start rounded-full bg-secondary px-2 py-0.5 active:opacity-70"
          >
            <Text className="text-xs text-muted-foreground" numberOfLines={1}>
              {blockerText}
            </Text>
          </Pressable>
        ) : null}
      </View>
      {isActive ? (
        <CancelButton taskId={task.id} issueId={issueId} onCancel={onCancel} />
      ) : null}
    </Pressable>
  );
}

function confirmCancel(taskId: string, onCancel: (taskId: string) => void) {
  // Same copy as the trailing Cancel button below — long-press is a second
  // reachable path to the same destructive action, not a new one.
  Alert.alert(
    "Cancel task?",
    "The agent will stop after the current step.",
    [
      { text: "Keep running", style: "cancel" },
      {
        text: "Cancel task",
        style: "destructive",
        onPress: () => onCancel(taskId),
      },
    ],
  );
}

function StatusBadge({ task }: { task: AgentTask }) {
  const label = STATUS_LABEL[task.status] ?? task.status;
  const cls = STATUS_CLASS[task.status] ?? "text-muted-foreground";
  // For failed tasks, surface the failure_reason inline so users don't have
  // to drill in. Missing / empty / unrecognised stays as just "Failed".
  if (task.status === "failed") {
    const reasonLabel = runFailureBadgeLabel(task.failure_reason);
    if (reasonLabel) {
      return (
        <Text className={`text-xs ${cls}`}>
          {label} · {reasonLabel}
        </Text>
      );
    }
  }
  return <Text className={`text-xs ${cls}`}>{label}</Text>;
}

function CancelButton({
  taskId,
  issueId,
  onCancel,
}: {
  taskId: string;
  issueId: string;
  onCancel?: (taskId: string) => void;
}) {
  // Called unconditionally (rules of hooks) even in fleet mode, where
  // `onCancel` overrides it and this mutation is simply never triggered —
  // no query runs until `.mutate()` is called, so the unused instance
  // costs nothing beyond the hook call itself.
  const mutation = useCancelTask(issueId);
  const busy = onCancel ? false : mutation.isPending;

  const onPress = () => confirmCancel(taskId, onCancel ?? mutation.mutate);

  return (
    <Pressable
      onPress={onPress}
      disabled={busy}
      className="px-3 py-1.5 rounded-md bg-secondary active:opacity-70"
    >
      <Text className="text-xs font-medium text-foreground">Cancel</Text>
    </Pressable>
  );
}

const STATUS_LABEL: Record<AgentTask["status"], string> = {
  queued: "Queued",
  dispatched: "Starting",
  waiting_local_directory: "Waiting for directory",
  running: "Running",
  completed: "Done",
  failed: "Failed",
  cancelled: "Cancelled",
  deferred: "Deferred",
  paused: "Paused",
};

const STATUS_CLASS: Record<AgentTask["status"], string> = {
  queued: "text-muted-foreground",
  dispatched: "text-brand",
  waiting_local_directory: "text-muted-foreground",
  running: "text-brand",
  completed: "text-muted-foreground",
  failed: "text-destructive",
  cancelled: "text-muted-foreground",
  deferred: "text-muted-foreground",
  paused: "text-muted-foreground",
};
