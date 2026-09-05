import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

// Why search (K55): a plain-language question finds the comment, run message
// or decision record that answers it. Only multi-word (or "?") queries of at
// least three characters are sent: single words belong to the issue search.

export interface WhySearchResult {
  id: string;
  source_type: "comment" | "task_message" | "decision_record" | (string & {});
  source_id: string;
  issue_id: string | null;
  issue_identifier?: string;
  issue_title?: string;
  snippet: string;
  score: number;
  created_at: string;
}

export function isWhyQuery(q: string): boolean {
  const t = q.trim();
  return t.length >= 3 && (/\s/.test(t) || t.endsWith("?"));
}

/** ts_headline marks hits with <b>; the UI highlights itself. */
export function stripHeadlineMarks(snippet: string): string {
  return snippet.replace(/<\/?b>/g, "");
}

export function whySearchOptions(wsId: string, q: string) {
  const query = q.trim();
  return queryOptions({
    queryKey: ["search", wsId, "why", query] as const,
    queryFn: ({ signal }) => api.searchWhy(query, { signal }),
    enabled: isWhyQuery(query),
    staleTime: 30_000,
  });
}
