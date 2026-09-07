import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/**
 * Dependency edges among the issues currently on a Gantt canvas (F30).
 *
 * Keyed by the SORTED id set rather than the render order: the same rows in a
 * different order are the same graph, and keying on order would refetch on
 * every re-sort. `issue_dependencies:changed` invalidates the whole prefix.
 */
export const issueDependencyEdgeKeys = {
  all: (wsId: string) => ["issue-dependency-edges", wsId] as const,
  forIssues: (wsId: string, issueIds: string[]) =>
    [...issueDependencyEdgeKeys.all(wsId), [...issueIds].sort().join(",")] as const,
};

export function issueDependencyEdgesOptions(wsId: string, issueIds: string[]) {
  return queryOptions({
    queryKey: issueDependencyEdgeKeys.forIssues(wsId, issueIds),
    queryFn: () => api.listIssueDependencyEdges(issueIds),
    // The arrow layer is decoration over rows the canvas already has: it must
    // never block or re-trigger the canvas itself, so it keeps its own generous
    // staleness and relies on the realtime event for freshness.
    staleTime: 60_000,
  });
}
