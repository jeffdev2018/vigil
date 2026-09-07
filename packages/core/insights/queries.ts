import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { InsightQuery } from "./schemas";
import { insightQueryHash } from "./shape";

export const insightKeys = {
  all: (wsId: string) => ["insights", wsId] as const,
  widgets: (wsId: string) => [...insightKeys.all(wsId), "widgets"] as const,
  /**
   * A run is keyed by the document, not by the widget: two widgets pinned on
   * the same document share one cache entry and one request.
   */
  run: (wsId: string, queryHash: string) => [...insightKeys.all(wsId), "run", queryHash] as const,
};

export function insightWidgetsOptions(wsId: string) {
  return queryOptions({
    queryKey: insightKeys.widgets(wsId),
    queryFn: ({ signal }) => api.listInsightWidgets({ signal }),
  });
}

/**
 * Execute one document. No LLM is involved: this is the call a pinned widget
 * makes on every refresh, which is what keeps a pinned figure meaning the same
 * thing it meant when it was pinned.
 */
export function insightRunOptions(wsId: string, query: InsightQuery | null | undefined) {
  const hash = insightQueryHash(query);
  return queryOptions({
    queryKey: insightKeys.run(wsId, hash),
    queryFn: ({ signal }) => api.runInsight(query as InsightQuery, { signal }),
    enabled: hash !== "",
    // A rollup does not change between two glances at the same card.
    staleTime: 60_000,
  });
}
