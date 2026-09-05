/**
 * Single row inside the agent-runs formSheet route
 * (`app/(app)/[workspace]/issue/[id]/runs.tsx`). Same component for active
 * and past tasks —
 * the trailing Cancel button is conditional on `status in {queued,
 * dispatched, running}`, and the status badge / colour swaps based on the
 * AgentTask.status enum.
 *
 * Tapping a running or finished row opens the read-only run replay
 * (`issue/[id]/replay/[taskId]`, k70). Queued / waiting rows have no event
 * log yet, so they stay inert.
 */
import { Alert, Pressable, View } from "react-native";
import { router } from "expo-router";
import type { AgentTask } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { useCancelTask } from "@/data/mutations/issues";
import { useActorLookup } from "@/data/use-actor-name";
import { useWorkspaceStore } from "@/data/workspace-store";
import { runFailureBadgeLabel } from "@/lib/run-failure-badge";
import { timeAgo } from "@/lib/time-ago";

interface Props {
  task: AgentTask;
  issueId: string;
}

const ACTIVE_STATUSES: readonly AgentTask["status"][] = [
  "queued",
  "dispatched",
  "running",
];

const REPLAYABLE_STATUSES: readonly AgentTask["status"][] = [
  "running",
  "completed",
  "failed",
  "cancelled",
];

export function RunRow({ task, issueId }: Props) {
  const { getName } = useActorLookup();
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const isActive = ACTIVE_STATUSES.includes(task.status);
  const canReplay = REPLAYABLE_STATUSES.includes(task.status);
  const openReplay = () => {
    if (!wsSlug) return;
    router.push({
      pathname: "/[workspace]/issue/[id]/replay/[taskId]",
      params: { workspace: wsSlug, id: issueId, taskId: task.id },
    });
  };
  const summary = task.trigger_summary?.trim() || fallbackSummary(task);
  // Past tasks use completed_at when present (server fills it for terminal
  // statuses); active tasks fall back to created_at so the user sees how
  // long it's been waiting.
  const timestamp = task.completed_at || task.created_at;

  return (
    <Pressable
      onPress={canReplay ? openReplay : undefined}
      disabled={!canReplay}
      className="flex-row items-start gap-3 py-2 -mx-2 px-2 rounded-lg active:bg-secondary"
    >
      <ActorAvatar type="agent" id={task.agent_id} size={28} showPresence />
      <View className="flex-1 gap-1">
        <Text
          className="text-sm text-foreground"
          numberOfLines={2}
        >
          <Text className="font-medium">{getName("agent", task.agent_id)}</Text>
          <Text className="text-muted-foreground"> · {summary}</Text>
        </Text>
        <View className="flex-row items-center gap-2">
          <StatusBadge task={task} />
          <Text className="text-xs text-muted-foreground">
            {timestamp ? timeAgo(timestamp) : ""}
          </Text>
          {canReplay ? (
            <Text className="text-xs text-brand">Replay ›</Text>
          ) : null}
        </View>
      </View>
      {isActive ? <CancelButton taskId={task.id} issueId={issueId} /> : null}
    </Pressable>
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
}: {
  taskId: string;
  issueId: string;
}) {
  const mutation = useCancelTask(issueId);

  const onPress = () => {
    Alert.alert(
      "Cancel task?",
      "The agent will stop after the current step.",
      [
        { text: "Keep running", style: "cancel" },
        {
          text: "Cancel task",
          style: "destructive",
          onPress: () => mutation.mutate(taskId),
        },
      ],
    );
  };

  return (
    <Pressable
      onPress={onPress}
      disabled={mutation.isPending}
      className="px-3 py-1.5 rounded-md bg-secondary active:opacity-70"
    >
      <Text className="text-xs font-medium text-foreground">Cancel</Text>
    </Pressable>
  );
}

function fallbackSummary(task: AgentTask): string {
  switch (task.kind) {
    case "comment":
      return "Comment task";
    case "autopilot":
      return "Autopilot run";
    case "chat":
      return "Chat task";
    case "quick_create":
      return "Quick create";
    case "direct":
    default:
      return "Task";
  }
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
