/**
 * Inline approvals — settle mutations not already covered elsewhere:
 *
 *   - Decision Cards: reuse `useRespondInboxDecision` (data/mutations/
 *     inbox.ts) — same endpoint, already wired.
 *   - Goal-loop questions: reuse `useAnswerIssueGoal` (data/mutations/
 *     issue-goal.ts) — same endpoint, already wired.
 *   - Held transitions: no mobile mutation existed yet — added here.
 *
 * Mirrors packages/core/issue-transitions/mutations.ts
 * useDecideIssueTransitionRequest: non-optimistic (the server, not this
 * client, decides whether the transition applies), so settle just
 * invalidates — the issue detail/timeline (status may have moved) and the
 * transition-requests list.
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/data/api";
import { issueKeys } from "@/data/queries/issue-keys";
import { useWorkspaceStore } from "@/data/workspace-store";

export function useDecideIssueTransitionApproval(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return useMutation({
    mutationFn: (v: {
      requestId: string;
      decision: "approve" | "reject";
      note?: string;
    }) => api.decideIssueTransitionRequest(v.requestId, v.decision, v.note),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.timeline(wsId, issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
    },
  });
}
