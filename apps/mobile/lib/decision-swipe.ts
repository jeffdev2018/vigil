/**
 * Pure helpers for the swipe-to-review Decision Card list (K36).
 *
 * Mirrors packages/views/inbox/components/decisions-view.tsx and the English
 * strings in packages/views/locales/en/inbox.json: the same urgency map (with
 * every unknown value coerced to "normal", per the enum-fallback rule in the
 * root CLAUDE.md), the same "due {when}" deadline phrasing off the shared
 * time-ago algorithm, the same "· recommended" marker.
 *
 * Behavioral parity (apps/mobile/CLAUDE.md): the swipe is an interaction
 * shortcut over web's option buttons, never a different answer. A right swipe
 * sends exactly what tapping the highlighted button sends on web — the
 * recommended option, or the first one when the server recommended none —
 * through the same respond endpoint (K01). No option, no swipe answer.
 */
import type { InboxDecision } from "@/data/schemas";
import { timeAgo } from "@/lib/time-ago";

/** The answer body POST /api/issues/{id}/decisions/{id}/respond accepts. */
export interface DecisionAnswer {
  option_id?: string;
  modified_text?: string;
}

/**
 * What a right swipe sends. `null` when the card carries no option at all —
 * such a card can only be answered with free text, so the swipe must not
 * invent one.
 */
export function swipeRightAnswer(
  decision: InboxDecision["decision"],
): DecisionAnswer | null {
  const options = decision.options ?? [];
  if (options.length === 0) return null;
  const recommended = options.find(
    (o) => o.id === decision.recommended_option_id,
  );
  const chosen = recommended ?? options[0];
  return chosen?.id ? { option_id: chosen.id } : null;
}

/** The label a right swipe should show, so the gesture is never blind. */
export function swipeRightLabel(
  decision: InboxDecision["decision"],
): string | null {
  const options = decision.options ?? [];
  if (options.length === 0) return null;
  const recommended = options.find(
    (o) => o.id === decision.recommended_option_id,
  );
  return (recommended ?? options[0])?.label || null;
}

export interface CondensedSummary {
  /** inbox.json `decisions.urgency.*`. */
  urgencyLabel: string;
  /** inbox.json `decisions.due`, or null when the card has no SLA. */
  deadlineText: string | null;
  /** One line per option: label, "· recommended", then its impact. */
  optionLines: string[];
}

/**
 * The card condensed to what a thumb needs before swiping: how urgent, by
 * when, and what the options actually do.
 */
export function condensedSummary(
  decision: InboxDecision["decision"],
): CondensedSummary {
  const urgency = decision.urgency;
  return {
    urgencyLabel:
      urgency === "high" ? "urgent" : urgency === "low" ? "low" : "normal",
    // Same phrasing and same helper as web's `t(decisions.due, { when:
    // timeAgo(sla_deadline_at) })`. timeAgo reads a future timestamp as
    // "Just now" on both clients; the wording is deliberately identical
    // rather than locally corrected, so the two never disagree.
    deadlineText: decision.sla_deadline_at
      ? `due ${timeAgo(decision.sla_deadline_at)}`
      : null,
    optionLines: (decision.options ?? []).map((o) => {
      const label = o.id === decision.recommended_option_id
        ? `${o.label} · recommended`
        : o.label;
      return o.impact ? `${label} — ${o.impact}` : label;
    }),
  };
}
