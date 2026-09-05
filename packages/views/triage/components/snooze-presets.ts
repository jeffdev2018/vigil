/** The preset offsets the Snooze menu offers, in menu order. */
export type SnoozePreset = "hour" | "evening" | "tomorrow" | "next_monday";

export const SNOOZE_PRESETS: SnoozePreset[] = [
  "hour",
  "evening",
  "tomorrow",
  "next_monday",
];

/** Server contract: a snooze must be in the future and at most 30 days out. */
export const MAX_SNOOZE_DAYS = 30;

function at(base: Date, dayOffset: number, hour: number): Date {
  const d = new Date(base);
  d.setDate(d.getDate() + dayOffset);
  d.setHours(hour, 0, 0, 0);
  return d;
}

/**
 * Resolve a preset to a concrete local time. "This evening" is 18:00 today,
 * or tomorrow evening once 18:00 has passed — a preset that resolves to the
 * past would be rejected by the server and read as a broken button.
 */
export function resolveSnoozePreset(preset: SnoozePreset, now: Date): Date {
  switch (preset) {
    case "hour":
      return new Date(now.getTime() + 60 * 60 * 1000);
    case "evening": {
      const evening = at(now, 0, 18);
      return evening > now ? evening : at(now, 1, 18);
    }
    case "tomorrow":
      return at(now, 1, 9);
    case "next_monday": {
      // 1 = Monday. Today-is-Monday still means NEXT Monday, not this second.
      const daysUntilMonday = ((1 - now.getDay() + 7) % 7) || 7;
      return at(now, daysUntilMonday, 9);
    }
    default:
      return new Date(now.getTime() + 60 * 60 * 1000);
  }
}

/**
 * Validate a custom `datetime-local` value against the same window the server
 * enforces. Returns the ISO string to send, or null when it is unusable.
 */
export function customSnoozeIso(value: string, now: Date): string | null {
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return null;
  if (parsed <= now) return null;
  if (parsed.getTime() - now.getTime() > MAX_SNOOZE_DAYS * 24 * 60 * 60 * 1000) return null;
  return parsed.toISOString();
}

/**
 * True while the item is parked in the future. The server already hides these
 * from the default pending listing; the Snoozed tab asks for them back and
 * needs to show only those, not the due ones that came with them.
 */
export function isSnoozed(
  item: { snoozed_until?: string | null },
  now: number = Date.now(),
): boolean {
  if (!item.snoozed_until) return false;
  const parked = new Date(item.snoozed_until).getTime();
  return !Number.isNaN(parked) && parked > now;
}
