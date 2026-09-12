import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, errorCode } from "../api";
import { isRunSettled, runStateOf } from "../agents/run-state";
import type { AgentTask } from "../types/agent";
import { issueKeys } from "./queries";

// Worktree run branch lifecycle (JEF-255): every run executes on its own
// branch in a dedicated worktree, and a finished run's row offers the two
// ways to close that branch out — promote it (push + open a PR) or discard it
// (delete branch and worktree). Both are requests the daemon executes
// asynchronously: the POST only enqueues, and the outcome lands on the task
// row itself (`promoted_at` / `promote_pr_url` / `discarded_at` /
// `pending_branch_action`), which is why the mutations here invalidate the
// issue's task list and the UI polls it while an action is in flight.

export const runActionKeys = {
  diff: (taskId: string) => ["run-diff", taskId] as const,
};

/**
 * A promote needs a branch left to promote: a terminal worktree run that has
 * neither been promoted nor discarded and has no branch action in flight.
 * Anything an older backend omits (branch_name, the markers) fails closed to
 * "no action", the same rule `revertable` follows.
 */
export function canPromoteRun(task: AgentTask): boolean {
  if (!isRunSettled(runStateOf(task.status))) return false;
  if (!task.branch_name) return false;
  if (task.promoted_at || task.discarded_at) return false;
  return !task.pending_branch_action;
}

/** Same gate as promote: the branch must exist and be unactioned. */
export function canDiscardRun(task: AgentTask): boolean {
  if (!isRunSettled(runStateOf(task.status))) return false;
  if (!task.branch_name) return false;
  if (task.promoted_at || task.discarded_at) return false;
  return !task.pending_branch_action;
}

export type RunBranchActionErrorKind =
  | "not_promotable"
  | "not_discardable"
  | "action_pending"
  | "generic";

/**
 * Maps a failed promote/discard onto a sentence the UI owns. All three 409s
 * mean the row the user acted on was stale — the branch already had an end
 * state, or another action beat this one — so the caller re-reads the task
 * list and shows an inline note rather than a raw error.
 */
export function runBranchActionErrorKind(err: unknown): RunBranchActionErrorKind {
  switch (errorCode(err)) {
    case "run_not_promotable":
      return "not_promotable";
    case "run_not_discardable":
      return "not_discardable";
    case "run_branch_action_pending":
      return "action_pending";
    default:
      return "generic";
  }
}

/** The diff endpoint's "nothing recorded" answer is a 404, not an empty body. */
export function isRunDiffNotFound(err: unknown): boolean {
  return errorCode(err) === "run_diff_not_found";
}

/**
 * The run's recorded patch, fetched only when the row's diff block is
 * expanded. A terminal run's diff is immutable once written, so nothing
 * refetches it; the 404 is not retried because no amount of waiting records
 * a diff the run never wrote.
 */
export function runDiffOptions(taskId: string, enabled: boolean) {
  return queryOptions({
    queryKey: runActionKeys.diff(taskId),
    queryFn: () => api.getRunDiff(taskId),
    enabled,
    staleTime: Number.POSITIVE_INFINITY,
    retry: (failureCount, err) => !isRunDiffNotFound(err) && failureCount < 3,
  });
}

function invalidateIssueTasks(qc: ReturnType<typeof useQueryClient>, issueId: string) {
  qc.invalidateQueries({ queryKey: issueKeys.tasks(issueId) });
}

export function usePromoteRun(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ taskId }: { taskId: string }) => api.promoteRun(issueId, taskId),
    onSettled: () => invalidateIssueTasks(qc, issueId),
  });
}

export function useDiscardRun(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ taskId }: { taskId: string }) => api.discardRun(issueId, taskId),
    onSettled: () => invalidateIssueTasks(qc, issueId),
  });
}
