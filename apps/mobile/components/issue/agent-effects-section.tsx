/**
 * "Agent changes" — what each agent run changed on this issue, newest run
 * first, with undo per run or per effect (K69).
 *
 * Mirrors packages/views/issues/components/agent-effects-section.tsx:
 * grouping / state / labels come from lib/agent-effects-display.ts, the
 * section is hidden when no run touched the issue, and undo buttons follow
 * web's `canManage` default (true — web does not gate them by role either;
 * mobile has no issue-edit permission helper to reuse).
 *
 * Divergences: web shows three toasts after an undo, mobile shows one
 * native `Alert.alert` with the same lines; the whole-run undo asks for a
 * confirm first because it is a bulk write and a mis-tap is likelier on a
 * phone (apps/mobile/CLAUDE.md §UI components, "Confirm → Alert.alert").
 */
import { Alert, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import type { AgentEffect, UndoReport } from "@/data/schemas";
import { issueAgentEffectsOptions } from "@/data/queries/agent-effects";
import {
  useUndoAgentEffect,
  useUndoTask,
} from "@/data/mutations/agent-effects";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  EFFECT_STATE_LABEL,
  describeEffect,
  effectState,
  groupEffectsByRun,
  undoReportLines,
} from "@/lib/agent-effects-display";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import { cn } from "@/lib/utils";

function report(r: UndoReport) {
  const lines = undoReportLines(r);
  if (lines.length > 0) Alert.alert("Agent changes", lines.join("\n"));
}

function onError(e: unknown) {
  Alert.alert(
    "Undo failed",
    e instanceof Error && e.message ? e.message : undefined,
  );
}

export function AgentEffectsSection({ issueId }: { issueId: string }) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data } = useQuery(issueAgentEffectsOptions(wsId, issueId));
  const undoTask = useUndoTask(issueId);
  const undoEffect = useUndoAgentEffect(issueId);
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;

  const effects = data?.effects ?? [];
  if (effects.length === 0) return null;
  const runs = groupEffectsByRun(effects);
  const busy = undoTask.isPending || undoEffect.isPending;

  const confirmUndoRun = (taskId: string, count: number) =>
    Alert.alert(
      "Undo this run?",
      `${count} change(s) by the agent will be reversed.`,
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Undo",
          style: "destructive",
          onPress: () =>
            undoTask.mutate(taskId, { onSuccess: report, onError }),
        },
      ],
    );

  return (
    <View className="px-4 pt-4 pb-2 border-t border-border gap-3">
      <View className="flex-row items-center gap-2">
        <Ionicons name="arrow-undo-outline" size={14} color={mutedFg} />
        <Text className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
          Agent changes
        </Text>
        <Text className="text-xs text-muted-foreground">
          reversible for {data?.window_hours ?? 24} h
        </Text>
      </View>
      {runs.map((run) => (
        <View key={run.task_id} className="gap-1.5">
          <View className="flex-row items-center justify-between gap-2">
            <Text className="text-sm font-medium flex-1" numberOfLines={1}>
              Run by {run.agent_name}
            </Text>
            {run.pending > 0 && (
              <Button
                size="sm"
                variant="outline"
                disabled={busy}
                onPress={() => confirmUndoRun(run.task_id, run.pending)}
              >
                <Text>Undo this run ({run.pending})</Text>
              </Button>
            )}
          </View>
          {run.effects.map((e) => (
            <EffectRow
              key={e.id}
              effect={e}
              busy={busy}
              onUndo={() =>
                undoEffect.mutate(e.id, { onSuccess: report, onError })
              }
            />
          ))}
        </View>
      ))}
    </View>
  );
}

function EffectRow({
  effect,
  busy,
  onUndo,
}: {
  effect: AgentEffect;
  busy: boolean;
  onUndo: () => void;
}) {
  const state = effectState(effect);
  const struck = state === "reversed" || state === "rejected";
  return (
    <View className="flex-row items-center gap-2">
      <Text
        className={cn(
          "text-sm flex-1",
          state !== "pending" && "text-muted-foreground",
          struck && "line-through",
        )}
        numberOfLines={2}
      >
        {describeEffect(effect)}
      </Text>
      {state !== "pending" ? (
        <Text className="text-xs text-muted-foreground rounded bg-muted px-1">
          {EFFECT_STATE_LABEL[state]}
        </Text>
      ) : (
        <Button size="sm" variant="ghost" disabled={busy} onPress={onUndo}>
          <Text>Undo</Text>
        </Button>
      )}
    </View>
  );
}
