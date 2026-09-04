/**
 * Meetings: a recording is uploaded segment by segment, transcribed as it
 * arrives, then summarized into a markdown summary plus one pending triage
 * item per action item. Audio is never stored server-side.
 */

/** Server-driven lifecycle of one meeting. */
export type MeetingStatus = "recording" | "summarizing" | "done" | "failed";

/** One extracted action item, as the triage entry it became. */
export interface MeetingAction {
  triage_item_id: string;
  title: string;
  /** Loose on purpose: a triage state added server-side must not fail the parse. */
  state: string;
  /** Set once the triage item has been accepted into an issue. */
  issue_id?: string;
}

export interface Meeting {
  id: string;
  title: string;
  /** Conferencing app the recording came from ("Zoom", "Meet", …), may be empty. */
  app_name: string;
  /** Loose on purpose: a status added server-side must not fail the parse. */
  status: MeetingStatus | (string & {});
  /** Omitted (empty) by the list endpoint — only the detail endpoint carries it. */
  transcript: string;
  summary_markdown: string;
  segment_count: number;
  created_by: string;
  started_at: string;
  ended_at?: string;
  actions: MeetingAction[];
  /** Number of action items, from the list endpoint only (it omits `actions`). */
  action_count: number;
  /** True when the meeting finished without an LLM: transcript kept, no actions. */
  summary_unavailable: boolean;
}

export interface MeetingListResponse {
  meetings: Meeting[];
}

/** Reply to one uploaded audio segment. `seq` echoes what the client sent. */
export interface MeetingSegmentResponse {
  seq: string;
  text: string;
  segment_count: number;
}
