import { keepPreviousData, queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { ReviewFlagFilter } from "./schemas";

export const reviewFlagKeys = {
  all: (wsId: string) => ["review-flags", wsId] as const,
  list: (wsId: string, issueId: string, filter: ReviewFlagFilter) =>
    ["review-flags", wsId, issueId, filter] as const,
};

/**
 * Flags arrive from agent runs, so the list changes without the reviewer doing
 * anything. The realtime `review_flag:changed` event is the fast path; there is
 * no poll floor because a flag is not time-critical the way a run's state is.
 */
export function reviewFlagsOptions(wsId: string, issueId: string, filter: ReviewFlagFilter = "open") {
  return queryOptions({
    queryKey: reviewFlagKeys.list(wsId, issueId, filter),
    queryFn: () => api.listReviewFlags(issueId, filter),
    enabled: !!wsId && !!issueId,
    // The open/all switch changes the key. Without this the section would
    // blank out and re-expand on every toggle, which reads as a bug rather
    // than as a filter.
    placeholderData: keepPreviousData,
  });
}
