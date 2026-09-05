/**
 * Pure display helpers for postmortems.
 *
 * Mirrors packages/views/postmortem/components/postmortem-page.tsx
 * (`formatCost`, the `draft | approved | discarded` filter order) and the
 * English strings in packages/views/locales/en/postmortem.json. Mobile is
 * English-only today; when mobile ships i18n, mirror that namespace.
 */
import type { PostmortemState } from "@multica/core/types";

/** The three states the list endpoint serves, in web's filter order. */
export const POSTMORTEM_STATES: PostmortemState[] = [
  "draft",
  "approved",
  "discarded",
];

/** Mirrors postmortem.json `filter.*`. */
export const POSTMORTEM_STATE_LABEL: Record<PostmortemState, string> = {
  draft: "Draft",
  approved: "Approved",
  discarded: "Discarded",
};

/** A state the server may add later still needs a label. */
export function postmortemStateLabel(state: string): string {
  return POSTMORTEM_STATE_LABEL[state as PostmortemState] ?? state;
}

/**
 * Provider-reported cost in 1e-10 USD ticks → a short USD string.
 * Byte-identical to web's `formatCost`, including the sub-cent 4-decimal
 * branch — the same run must not read as "$0.00" on one client and
 * "$0.0007" on the other.
 */
export function formatPostmortemCost(ticks: number): string {
  const usd = ticks / 1e10;
  return `$${usd.toFixed(usd < 0.01 ? 4 : 2)}`;
}

/**
 * How the draft was written. Web shows this as an icon plus tooltip
 * (Sparkles / Wand2); a phone has no hover, so mobile spells it out.
 */
export function postmortemOriginLabel(llmGenerated: boolean): string {
  return llmGenerated ? "AI-drafted" : "Auto-filled from run facts";
}

/** Per-state empty copy. Mirrors postmortem.json `list.empty_description_*`. */
export function postmortemEmptyMessage(state: PostmortemState): string {
  switch (state) {
    case "draft":
      return "No drafts waiting. When an agent run fails, a postmortem is drafted and waits here for your review.";
    case "approved":
      return "Nothing approved yet. Postmortems you approve are kept here.";
    case "discarded":
      return "Nothing discarded yet. Postmortems you discard are moved here.";
  }
}
