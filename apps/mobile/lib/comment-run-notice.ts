/**
 * Pre-send run notice for the mobile comment composer.
 *
 * The list of agents a send would start is the server's answer
 * (POST /api/issues/{id}/comments/trigger-preview), exactly what web's
 * CommentTriggerChips render. The signature mirrors
 * packages/views/issues/hooks/use-comment-trigger-preview.ts
 * `commentTriggerPreviewSignature`, so typing only refetches when the
 * mention set or emptiness changes. The cost line mirrors web's
 * AgentRunDetails (English-only on mobile).
 */
import { parseMentions } from "@multica/core/issues/comment-trigger-outcomes";
import { formatPostmortemCost } from "./postmortem-display";

const NOTE_COMMAND_RE = /^\/note(?:$|\s)/i;

export function commentTriggerSignature(content: string): string {
  if (!content.trim() || NOTE_COMMAND_RE.test(content.replace(/^[ \t\r\n]+/, ""))) return "empty";
  const seen = new Set<string>();
  for (const { type, id } of parseMentions(content)) {
    if (type !== "issue") seen.add(`${type}:${id}`);
  }
  return `nonempty|${[...seen].join(",")}`;
}

export function agentRunNoticeText(input: {
  name: string;
  runtimeName: string | null;
  avgCostUsdTicks: number | null;
  sampleRuns: number;
  costPending: boolean;
}): string {
  const cost = typeof input.avgCostUsdTicks === "number"
    ? `≈ ${formatPostmortemCost(input.avgCostUsdTicks)} per run (${input.sampleRuns === 1 ? "its last priced run" : `average of its last ${input.sampleRuns} priced runs`})`
    : input.costPending
      ? "estimating cost…"
      : "cost unknown (no priced run yet)";
  return `${input.name} will start a run when you send · runs on ${input.runtimeName ?? "a runtime you cannot see"} · ${cost}`;
}
