import type { InsightQuery, InsightRow } from "./schemas";

/**
 * The chart form an insight should take. Pure, so it is the canonical place
 * this decision is tested — the component suite renders the happy path and
 * points here.
 *
 * The server derives the same value and sends it as `shape`. This function
 * exists for the two cases where the server's answer is not usable: a widget
 * rendered from its stored document before any run has returned, and a
 * response whose `shape` is a value this build does not know.
 */
export type InsightShape = "number" | "bar" | "line" | "donut";

const KNOWN_SHAPES = new Set<string>(["number", "bar", "line", "donut"]);

export function deriveInsightShape(query: InsightQuery | null | undefined): InsightShape {
  const groupBy = query?.group_by ?? [];
  if (groupBy.length === 0) return "number";
  if (groupBy.length > 1) return "bar";
  if (groupBy[0] === "time") return "line";
  return query?.metric === "count" ? "donut" : "bar";
}

/**
 * Resolve the shape actually used for rendering. A server value the client
 * does not know is not an error — the backend may be newer — so it falls back
 * to the local derivation rather than to a blank card.
 */
export function resolveInsightShape(
  serverShape: string | null | undefined,
  query: InsightQuery | null | undefined,
): InsightShape {
  if (serverShape && KNOWN_SHAPES.has(serverShape)) return serverShape as InsightShape;
  return deriveInsightShape(query);
}

/**
 * The row key carrying the measured value, and the one carrying the label.
 * Rows are keyed by dimension name, so the label column is whatever the
 * document grouped by.
 */
export function insightLabelKey(query: InsightQuery | null | undefined): string | null {
  const groupBy = query?.group_by ?? [];
  return groupBy.length > 0 ? (groupBy[0] ?? null) : null;
}

/** The numeric value of one row, or null when the aggregate produced none. */
export function insightRowValue(row: InsightRow | undefined): number | null {
  const raw = row?.["value"];
  return typeof raw === "number" ? raw : null;
}

/**
 * A stable string for a document, used as the React Query key of a run. Keys
 * are sorted recursively so two structurally identical documents that were
 * built in a different order share one cache entry instead of running twice.
 */
export function insightQueryHash(query: InsightQuery | null | undefined): string {
  if (!query) return "";
  return stableStringify(query);
}

function stableStringify(value: unknown): string {
  if (value === null || typeof value !== "object") return JSON.stringify(value) ?? "null";
  if (Array.isArray(value)) return `[${value.map(stableStringify).join(",")}]`;
  const entries = Object.entries(value as Record<string, unknown>)
    .filter(([, v]) => v !== undefined)
    .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
  return `{${entries.map(([k, v]) => `${JSON.stringify(k)}:${stableStringify(v)}`).join(",")}}`;
}
