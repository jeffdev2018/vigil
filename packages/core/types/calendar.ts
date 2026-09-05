/**
 * Calendar subscription: the read-only ICS URL the user's calendar already
 * publishes. No OAuth and no write access — it answers one question, "what
 * meeting is happening right now", so a recording gets a name instead of a
 * timestamp.
 */

/** One occurrence, with absolute ISO times. */
export interface CalendarEvent {
  summary: string;
  /** The conferencing link the event carried, when it had one. */
  url?: string;
  start: string;
  end: string;
  /** True when the event covers the moment the server answered. */
  in_progress: boolean;
}

export interface CalendarUpcoming {
  events: CalendarEvent[];
  /** False when the user has no feed saved — not the same as a free day. */
  configured: boolean;
}

export interface CalendarFeed {
  /** Empty when nothing is subscribed. Always https:// (webcal is rewritten). */
  url: string;
  last_fetched_at?: string;
  /** Empty when the last fetch worked; the reason when it did not. */
  last_error?: string;
}
