import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";

// Cross-provider self-review (K15): another provider's structured report
// on the last diff, listed newest first; a failed review can be retried.

export interface CrossReviewReport {
  verdict: "approve" | "request_changes" | "comment";
  risks: string[];
  questions: string[];
  suggestions: string[];
  summary: string;
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

export const crossReviewKeys = {
  issue: (wsId: string, issueId: string) => ["cross-reviews", wsId, issueId] as const,
};

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
