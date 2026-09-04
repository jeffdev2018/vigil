import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { ReviewCockpit } from "../types";
import { issueKeys } from "./queries";

// Review cockpit (K16): one query, one screen.

export function reviewCockpitOptions(wsId: string, issueId: string, runId?: string) {
  return queryOptions({
    queryKey: issueKeys.cockpit(wsId, issueId, runId),
    queryFn: ({ signal }) => api.getReviewCockpit(issueId, runId, { signal }),
  });
}

/** Approval waits for CI: a pending check is the one blocker the reviewer cannot resolve by hand. */
export function cockpitChecksPending(c: Pick<ReviewCockpit, "merge_readiness">): boolean {
  return c.merge_readiness?.blockers.some((b) => b.kind === "checks_pending") === true;
}

export function usdFromTicks(ticks: number | null): number | null {
  return ticks === null ? null : ticks / 1e10;
}
