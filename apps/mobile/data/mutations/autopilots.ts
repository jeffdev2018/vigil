/**
 * Autopilots from a sentence (JEF-373).
 *
 * Mobile has no autopilots screen, so it exposes only the two verbs that
 * belong to an issue: draft (ask the model to read the sentence — writes
 * nothing) and propose (file the autopilot PAUSED, with a Decision Card on
 * the issue whose answer activates or discards it). Managing existing
 * autopilots stays on web/desktop.
 *
 * Neither is optimistic: draft has no cache to patch, and propose is a
 * server decision that produces a Decision Card the client cannot predict
 * (the card's id, options and SLA deadline are all server-side).
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type {
  DraftAutopilotInput,
  ProposeAutopilotInput,
} from "@multica/core/types";
import { api } from "@/data/api";
import { approvalKeys } from "@/data/queries/approvals";
import { issueKeys } from "@/data/queries/issue-keys";
import { useWorkspaceStore } from "@/data/workspace-store";

export function useDraftAutopilot() {
  return useMutation({
    mutationFn: (input: DraftAutopilotInput) => api.draftAutopilot(input),
  });
}

/**
 * The Decision Card lands on `issue_id`, so the issue's timeline and its
 * pending-asks feed both change. The autopilot itself is invisible on
 * mobile until someone answers the card, so there is no autopilot cache to
 * invalidate here.
 */
export function useProposeAutopilot(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return useMutation({
    mutationFn: (input: ProposeAutopilotInput) =>
      api.proposeAutopilot({ issue_id: issueId, ...input }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: approvalKeys.issue(wsId, issueId) });
      qc.invalidateQueries({ queryKey: approvalKeys.workspace(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.timeline(wsId, issueId) });
    },
  });
}
