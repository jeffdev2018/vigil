/**
 * Pure display helpers for the run replay screen
 * (`app/(app)/[workspace]/issue/[id]/replay/[taskId].tsx`).
 *
 * Mirrors the semantics of web's replay scrubber (built in parallel):
 * "so far" counts are cumulative up to and including the current position,
 * and the seal label is derived from the server's `sealed` block only —
 * the client never re-hashes the chain. Mobile is English-only today.
 */
import type { RunReplay, RunReplayEvent } from "@/data/schemas";

/** Human labels for every kind the server emits today; unknown kinds fall
 *  back to the raw wire value so a newer server still renders. */
const KIND_LABEL: Record<string, string> = {
  text: "Text",
  thinking: "Thinking",
  tool_use: "Tool call",
  tool_result: "Tool result",
  steer: "Steer",
  status: "Status",
  error: "Error",
  checkpoint: "Checkpoint",
  effect: "Effect",
  effect_reversed: "Effect reversed",
  decision_asked: "Decision asked",
  decision_answered: "Decision answered",
  handoff: "Handoff",
  cost: "Cost",
  audit: "Audit",
};

export function replayKindLabel(kind: string): string {
  return KIND_LABEL[kind] ?? kind;
}

export interface ReplayCounts {
  toolCalls: number;
  effects: number;
  decisions: number;
  steers: number;
}

/** Counts over events[0..index] inclusive. `index < 0` yields zeros. */
export function replayCountsSoFar(
  events: readonly RunReplayEvent[],
  index: number,
): ReplayCounts {
  const counts: ReplayCounts = { toolCalls: 0, effects: 0, decisions: 0, steers: 0 };
  const end = Math.min(index, events.length - 1);
  for (let i = 0; i <= end; i++) {
    switch (events[i].kind) {
      case "tool_use":
        counts.toolCalls++;
        break;
      case "effect":
        counts.effects++;
        break;
      case "decision_asked":
        counts.decisions++;
        break;
      case "steer":
        counts.steers++;
        break;
      default:
        break;
    }
  }
  return counts;
}

export type ReplaySealState = "verified" | "broken" | "unsealed";

export function replaySealState(sealed: RunReplay["sealed"]): ReplaySealState {
  if (!sealed) return "unsealed";
  return sealed.verified === true ? "verified" : "broken";
}

export function replaySealLabel(sealed: RunReplay["sealed"]): string {
  switch (replaySealState(sealed)) {
    case "verified":
      return "Sealed and verified";
    case "broken":
      return "Seal broken";
    case "unsealed":
      return "Not sealed yet";
  }
}

/** "1.2k in · 340 out" style token summary. */
export function formatReplayTokens(input: number, output: number): string {
  return `${compact(input)} in · ${compact(output)} out`;
}

function compact(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

/** Pretty JSON capped at `maxLines`; a trailing "…" line marks truncation.
 *  Empty/absent data yields an empty string so the card can skip the block. */
export function previewJson(
  data: Record<string, unknown> | null | undefined,
  maxLines = 12,
): string {
  if (!data || Object.keys(data).length === 0) return "";
  const lines = JSON.stringify(data, null, 2).split("\n");
  if (lines.length <= maxLines) return lines.join("\n");
  return [...lines.slice(0, maxLines), "…"].join("\n");
}
