import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";

// Cross-provider self-review (K15): another provider's structured report
// on the last diff, listed newest first; a failed review can be retried.

export interface CrossReviewChecklistResult {
  item: string;
  pass: boolean;
  note: string;
}

export interface CrossReviewReport {
  verdict: "approve" | "request_changes" | "comment";
  risks: string[];
  questions: string[];
  suggestions: string[];
  summary: string;
  /** Per-item verdicts against the project checklist (JEF-238). Absent on
   *  reports produced before the checklist existed. */
  checklist_results?: CrossReviewChecklistResult[];
}

/**
 * One-shot client-only signal raised by the cross_review:rework /
 * cross_review:escalated WS events (JEF-238), written to the query cache by
 * the realtime sync and rendered as a notice by the cross-review section.
 * The `at` nonce keeps a repeat signal from being deduped away.
 */
export interface CrossReviewSignal {
  kind: "rework" | "escalated";
  /** Rework: the cycle the task was sent back on. */
  cycle?: number;
  /** Escalated: the cycle cap that was reached. */
  cycles?: number;
  at: number;
}

export interface CrossReview {
  task_id: string;
  review_of_task_id: string;
  reviewer_agent_id: string;
  reviewer_name: string;
  reviewer_provider: string;
  status: string;
  report: CrossReviewReport | null;
  created_at: string;
  completed_at: string | null;
}

export interface CrossReviewSettings {
  enabled: boolean;
  opt_out_project_ids: string[];
}

export const crossReviewKeys = {
  issue: (wsId: string, issueId: string) => ["cross-reviews", wsId, issueId] as const,
  settings: (wsId: string) => ["cross-reviews", wsId, "settings"] as const,
  signal: (wsId: string, issueId: string) => ["cross-reviews", wsId, issueId, "signal"] as const,
};

/**
 * Reactive read of the client-only rework/escalated signal. Disabled query
 * over a cache cell the realtime sync writes with setQueryData — same pattern
 * as chatQuickActionsFailureOptions.
 */
export function crossReviewSignalOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: crossReviewKeys.signal(wsId, issueId),
    queryFn: async (): Promise<CrossReviewSignal | null> => null,
    enabled: false,
    staleTime: Infinity,
  });
}

export function crossReviewSettingsOptions(wsId: string) {
  return queryOptions({ queryKey: crossReviewKeys.settings(wsId), queryFn: () => api.getCrossReviewSettings() });
}

export function useSaveCrossReviewSettings(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: CrossReviewSettings) => api.putCrossReviewSettings(v),
    onSettled: () => qc.invalidateQueries({ queryKey: crossReviewKeys.settings(wsId) }),
  });
}

export function issueCrossReviewsOptions(wsId: string, issueId: string) {
  return queryOptions({ queryKey: crossReviewKeys.issue(wsId, issueId), queryFn: () => api.listCrossReviews(issueId), refetchInterval: 15_000 });
}

export function useRetryCrossReview(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.retryCrossReview(issueId),
    onSettled: () => qc.invalidateQueries({ queryKey: crossReviewKeys.issue(wsId, issueId) }),
  });
}

const activeStatuses = new Set(["queued", "dispatched", "running", "waiting_local_directory", "deferred", "paused"]);

/** in progress / failed / done, from the run status. */
export function crossReviewState(r: CrossReview): "in_progress" | "failed" | "done" {
  if (r.status === "failed" || r.status === "cancelled") return "failed";
  if (activeStatuses.has(r.status)) return "in_progress";
  return "done";
}
