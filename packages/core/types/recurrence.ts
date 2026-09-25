// Recurring issues (OS plan, table stakes): a rule lives on an issue (the
// "source") and every occurrence it spawns carries the same rule, so the same
// GET from any member of the series answers with it.
// Server source of truth: server/internal/handler/issue_recurrence.go.

/** Open on the wire like every other server enum — read it with a `default`. */
export type IssueRecurrenceMode = "schedule" | "on_close" | (string & {});

export interface IssueRecurrence {
  id: string;
  /** The source issue the rule was filed on. */
  issue_id: string;
  /** 5-field cron; "" for `on_close`. */
  cron_expression: string;
  /** IANA zone the cron is read in. */
  timezone: string;
  mode: IssueRecurrenceMode;
  enabled: boolean;
  /** RFC 3339; null when `on_close` or disabled. */
  next_run_at: string | null;
  last_occurrence_id: string | null;
  occurrence_count: number;
  created_by_type: string;
  created_by_id?: string | null;
  created_at: string;
  updated_at: string;
}

export interface IssueRecurrenceSource {
  id: string;
  identifier: string;
  title: string;
}

export interface IssueRecurrenceOccurrence {
  id: string;
  identifier: string;
  title: string;
  status: string;
  created_at: string;
  due_date: string | null;
}

export interface IssueRecurrenceResponse {
  recurrence: IssueRecurrence;
  source: IssueRecurrenceSource;
  /** Newest first, at most 20, the source included. */
  occurrences: IssueRecurrenceOccurrence[];
  /** Three RFC 3339 instants; empty for `on_close` or a disabled rule. */
  next_runs: string[];
}

export interface SetIssueRecurrenceInput {
  /** Required for `schedule`; ignored by `on_close`. */
  cron_expression?: string;
  /** IANA zone; the server defaults to UTC. */
  timezone?: string;
  /** Defaults to "schedule" server-side. */
  mode?: IssueRecurrenceMode;
  /** Defaults to true server-side. */
  enabled?: boolean;
}
