import { z } from "zod";
import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { AgentTask, SetIssueGoalInput } from "../types";
import { issueKeys } from "./queries";

// Goal loop: an agent works one issue toward a stated goal across bounded
// continuations, stopping when it is satisfied, stuck (no_progress), or needs
// the human (a pending question). Every run's contribution rides on
// `AgentTask.result.goal_loop`; the durable state lives on the issue's goal
// record, fetched/mutated through the endpoints below.

export const goalKeys = {
  /** PREFIX: every goal in the workspace, invalidated on an issue:aux_changed with no issue_id. */
  all: (wsId: string) => ["issue-goal", wsId] as const,
  issue: (wsId: string, issueId: string) => [...goalKeys.all(wsId), issueId] as const,
};

export function issueGoalOptions(wsId: string, issueId: string) {
  return queryOptions({ queryKey: goalKeys.issue(wsId, issueId), queryFn: () => api.getIssueGoal(issueId) });
}

// No optimistic updates here: unlike a status/assignee toggle, the outcome of
// setting/pausing/resuming/answering a goal is decided server-side (the
// server may immediately dispatch a continuation, or refuse an answer with
// 409 when no question is waiting) and none of these navigate away, so a
// settle-time invalidation is simpler and no less responsive.
export function useSetIssueGoal(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: SetIssueGoalInput) => api.setIssueGoal(issueId, body),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: goalKeys.issue(wsId, issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.tasks(issueId) });
    },
  });
}

export function usePauseIssueGoal(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.pauseIssueGoal(issueId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: goalKeys.issue(wsId, issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.tasks(issueId) });
    },
  });
}

export function useResumeIssueGoal(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.resumeIssueGoal(issueId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: goalKeys.issue(wsId, issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.tasks(issueId) });
    },
  });
}

export function useAnswerIssueGoal(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (answer: string) => api.answerIssueGoal(issueId, answer),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: goalKeys.issue(wsId, issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.tasks(issueId) });
    },
  });
}

/** One run's contribution to the loop, from `AgentTask.result.goal_loop`. */
export interface GoalRunVerdict {
  continuation: number;
  signature: string;
  no_progress: number;
  outcome: string;
  blocker?: string;
  reason?: string;
  evidence?: string[];
  next_step?: string;
}

const GoalRunVerdictSchema = z.object({
  continuation: z.number().catch(0).default(0),
  signature: z.string().catch("").default(""),
  no_progress: z.number().catch(0).default(0),
  outcome: z.string().catch("").default(""),
  blocker: z.string().optional().catch(undefined),
  reason: z.string().optional().catch(undefined),
  evidence: z.array(z.string()).optional().catch(undefined),
  next_step: z.string().optional().catch(undefined),
}).loose();

/**
 * `task.result` is `unknown` on the wire (JEF-274-style boundary): a run that
 * predates the goal loop, or one outside it, carries no `goal_loop` key at
 * all, and a malformed one must not throw — both read as "not a goal run".
 */
export function goalLoopOfTask(task: AgentTask): GoalRunVerdict | null {
  const result = task.result;
  if (!result || typeof result !== "object") return null;
  const raw = (result as Record<string, unknown>)["goal_loop"];
  if (!raw || typeof raw !== "object") return null;
  const parsed = GoalRunVerdictSchema.safeParse(raw);
  return parsed.success ? parsed.data : null;
}

const KNOWN_STOPPED_REASONS = new Set([
  "exhausted",
  "stagnation",
  "needs_user_input",
  "external_wait",
  "run_failed",
  "judge_unavailable",
  "paused",
  "issue_changed",
]);

/**
 * Locale key under `issues:goal_loop.outcomes` for a raw outcome string
 * ("" | "satisfied" | "continued" | "stopped:<why>"). An unrecognized
 * `<why>` (a newer backend added a stop reason) folds to `stopped_unknown`
 * rather than an unknown outcome shape folding to the generic `other`.
 */
const KNOWN_BLOCKERS = new Set(["goal_not_met_yet", "missing_evidence", "needs_user_input", "external_wait", "run_failed"]);

/** i18n key under goal_loop.blockers for a judge blocker; "other" for a newer server's token. */
export function goalBlockerLabelKey(blocker: string): string {
  return KNOWN_BLOCKERS.has(blocker) ? blocker : "other";
}

export function goalOutcomeLabelKey(outcome: string): string {
  if (outcome === "") return "none";
  if (outcome === "satisfied" || outcome === "continued") return outcome;
  if (outcome.startsWith("stopped:")) {
    const why = outcome.slice("stopped:".length);
    return KNOWN_STOPPED_REASONS.has(why) ? `stopped_${why}` : "stopped_unknown";
  }
  return "other";
}
