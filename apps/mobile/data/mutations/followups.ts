/**
 * Follow-up writes (JEF-373) — schedule and cancel a deferred wake-up of an
 * issue's agent.
 *
 * Nothing here is optimistic. Scheduling is a server decision (it resolves
 * the agent, enforces the 1-minute…30-day window and the per-agent /
 * per-workspace daily budget, and only then hands back the `fires_at` the
 * row will actually carry) — the outcome is not locally predictable, which
 * is the first condition of the optimistic gate in the root CLAUDE.md.
 * Cancel is a confirm flow behind a native Alert, and confirm flows await
 * the server.
 *
 * Both writes also move the runs list: a follow-up is a row of
 * `agent_task_queue` with status "deferred", so it shows on the fleet page
 * as a run blocked on "deferred".
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { ScheduleFollowupInput } from "@multica/core/types";
import { api } from "@/data/api";
import { followupKeys } from "@/data/queries/followups";
import { runsKeys } from "@/data/queries/runs";
import { useWorkspaceStore } from "@/data/workspace-store";

export function useScheduleFollowup(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return useMutation({
    mutationFn: (input: ScheduleFollowupInput) =>
      api.scheduleIssueFollowup(issueId, input),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: followupKeys.issue(wsId, issueId) });
      qc.invalidateQueries({ queryKey: runsKeys.all(wsId) });
    },
  });
}

export function useCancelFollowup(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return useMutation({
    mutationFn: (followupId: string) =>
      api.cancelIssueFollowup(issueId, followupId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: followupKeys.issue(wsId, issueId) });
      qc.invalidateQueries({ queryKey: runsKeys.all(wsId) });
    },
  });
}
