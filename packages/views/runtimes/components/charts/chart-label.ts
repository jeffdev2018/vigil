import type { ChartConfig } from "@multica/ui/components/ui/chart";

/**
 * Translated label for a Recharts series name.
 *
 * Recharts passes the Bar/Line's `dataKey` through as the tooltip item's
 * `name` (never the translated `name` prop some of these charts also pass —
 * a formatter that echoes it back renders the raw dataKey, e.g. "input" or
 * "totalSeconds"). Look it up in the same `ChartConfig` that drives the
 * legend/CSS vars instead, and fall back to the key itself if the config
 * ever loses an entry.
 *
 * Shared by every chart under runtimes/components/charts/ — factored out of
 * failure-class-visuals.ts (which re-exports it for its existing importers)
 * so the daily/weekly cost, tokens, tasks and time charts don't each carry
 * their own copy.
 */
export function labelOf(
  config: ChartConfig,
  name: string | number | undefined,
): string {
  const key = String(name ?? "");
  const label = config[key]?.label;
  return typeof label === "string" ? label : key;
}
