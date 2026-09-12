/**
 * The three quick answers to "when should the agent come back?", computed in
 * the caller's own timezone (the browser's), because "tomorrow 9:00" is a
 * wall-clock promise, not an instant: `Date`'s local getters/setters are what
 * makes 09:00 mean 09:00 where the person sits. The API takes RFC 3339, so
 * each choice resolves to an instant here and the server never guesses a zone.
 *
 * The server refuses anything under a minute or over 30 days ahead; all three
 * of these sit well inside that window.
 */
export type FollowupQuickChoice = "in_1h" | "tomorrow_9" | "next_monday_9";

export const FOLLOWUP_QUICK_CHOICES: readonly FollowupQuickChoice[] = [
  "in_1h",
  "tomorrow_9",
  "next_monday_9",
] as const;

/** Note cap the server enforces (500 runes). */
export const FOLLOWUP_NOTE_MAX = 500;

function atLocalNine(base: Date, addDays: number): Date {
  const d = new Date(base.getTime());
  d.setDate(d.getDate() + addDays);
  d.setHours(9, 0, 0, 0);
  return d;
}

/**
 * The instant a quick choice means, as RFC 3339. `now` is injectable so this
 * is testable without freezing the clock.
 *
 * "Next Monday" is the Monday of the coming week: on a Monday it means the
 * one seven days out, never nine hours ago.
 */
export function followupQuickChoiceInstant(
  choice: FollowupQuickChoice,
  now: Date = new Date(),
): string {
  switch (choice) {
    case "in_1h":
      return new Date(now.getTime() + 60 * 60 * 1000).toISOString();
    case "tomorrow_9":
      return atLocalNine(now, 1).toISOString();
    case "next_monday_9": {
      // getDay(): 0 = Sunday. Days until the next Monday, never 0.
      const daysAhead = ((8 - now.getDay()) % 7) || 7;
      return atLocalNine(now, daysAhead).toISOString();
    }
    default:
      return new Date(now.getTime() + 60 * 60 * 1000).toISOString();
  }
}

/** Seconds until an instant, or null when it is absent or unparseable. */
export function followupSecondsUntil(firesAt: string, now: number = Date.now()): number | null {
  const at = Date.parse(firesAt);
  if (Number.isNaN(at)) return null;
  return Math.max(0, Math.round((at - now) / 1000));
}
