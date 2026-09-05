/**
 * Undo a whole agent run or one effect (K69).
 *
 * Mirrors `useUndoTask` / `useUndoAgentEffect` in
 * packages/core/issues/agent-effects.ts: non-optimistic (the server decides
 * what it can reverse), then the effects list and the whole issue prefix
 * (detail + timeline + lists) are invalidated because a reversal rewrites
 * status / fields / comments.
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/data/api";
import type { UndoReport } from "@/data/schemas";
import { agentEffectKeys } from "@/data/queries/agent-effects";
import { issueKeys } from "@/data/queries/issue-keys";
import { useWorkspaceStore } from "@/data/workspace-store";

function useUndo(issueId: string, undo: (id: string) => Promise<UndoReport>) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return useMutation<UndoReport, Error, string>({
    mutationFn: undo,
    onSettled: () => {
      qc.invalidateQueries({ queryKey: agentEffectKeys.issue(wsId, issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

export function useUndoTask(issueId: string) {
  return useUndo(issueId, (taskId) => api.undoTask(taskId));
}

export function useUndoAgentEffect(issueId: string) {
  return useUndo(issueId, (effectId) => api.undoAgentEffect(effectId));
}
