/**
 * Native calendar (OS plan, chantier 19). Events with participants who are
 * members or agents; a member schedules directly, an agent proposes and a
 * person accepts through a Decision Card; the agenda joins events with issue
 * due dates, cycles and meetings; the slot finder respects busy time and
 * members' working hours. See server/internal/handler/calendar_events.go and
 * apps/docs/content/docs/calendar.mdx for the source of truth.
 *
 * This is the *inbound-and-outbound events* calendar. It is a different
 * feature from `@multica/core/calendar` (the ICS feed Multica reads to name a
 * recording after the meeting that is running) — do not conflate the two.
 */

/** Loose on purpose: an enum value added server-side must not fail the parse. */
export type CalendarEventStatus = "proposed" | "scheduled" | "cancelled" | (string & {});
export type CalendarParticipantType = "member" | "agent" | (string & {});
export type CalendarParticipantResponse =
  | "pending"
  | "accepted"
  | "declined"
  | "tentative"
  | (string & {});

export interface CalendarParticipant {
  type: CalendarParticipantType;
  id: string;
  name?: string;
  response: CalendarParticipantResponse;
  required: boolean;
}

export interface CalendarActor {
  type: CalendarParticipantType;
  id: string;
  name?: string;
}

export interface CalendarEventEntry {
  id: string;
  title: string;
  description: string;
  starts_at: string;
  ends_at: string;
  all_day: boolean;
  timezone: string;
  location: string;
  issue_id: string | null;
  issue_identifier?: string;
  project_id: string | null;
  status: CalendarEventStatus;
  created_by: CalendarActor;
  source: string;
  external_id?: string;
  decision_id: string | null;
  participants: CalendarParticipant[];
  created_at: string;
  updated_at: string;
}

export interface CalendarEventParticipantInput {
  type: "member" | "agent";
  id: string;
  required?: boolean;
}

/** Create/update body. */
export interface CalendarEventInput {
  title: string;
  description?: string;
  starts_at: string;
  ends_at: string;
  all_day?: boolean;
  timezone?: string;
  location?: string;
  issue_id?: string;
  project_id?: string;
  participants?: CalendarEventParticipantInput[];
}

export interface CalendarEventsResponse {
  events: CalendarEventEntry[];
  from: string;
  to: string;
}

export interface AgendaIssue {
  id: string;
  identifier: string;
  title: string;
  status: string;
  due_date: string;
  assignee_type?: string | null;
  assignee_id?: string | null;
}

export interface AgendaCycle {
  id: string;
  name: string;
  start_date: string;
  end_date: string;
}

export interface AgendaMeeting {
  id: string;
  title: string;
  status: string;
  started_at: string;
  ended_at?: string | null;
}

/** A scheduled wake-up of an issue's agent, in the unified agenda. */
export interface AgendaFollowup {
  id: string;
  issue_id: string;
  identifier: string;
  issue_title: string;
  agent_id: string;
  agent_name: string;
  fires_at: string;
  note: string;
}

export interface CalendarAgenda {
  from: string;
  to: string;
  events: CalendarEventEntry[];
  issues_due: AgendaIssue[];
  cycles: AgendaCycle[];
  meetings: AgendaMeeting[];
  /** Pending follow-ups in the window. Absent on older servers. */
  followups: AgendaFollowup[];
}

export interface CalendarSlot {
  starts_at: string;
  ends_at: string;
}

export interface CalendarSlotsResponse {
  slots: CalendarSlot[];
  duration_minutes: number;
  tz: string;
}

/** GET .../calendar/feed-token. */
export interface CalendarFeedTokenStatus {
  configured: boolean;
  created_at?: string;
}

/** POST .../calendar/feed-token — the clear URL is in this response only. */
export interface CalendarFeedTokenMinted {
  url: string;
  path: string;
}

export interface CalendarGoogleImportResult {
  created: number;
  updated: number;
  seen: number;
}
