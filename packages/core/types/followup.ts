// Follow-ups (OS plan, vague B, "réveil programmé"): a deferred wake-up of an
// issue's agent. Server source of truth: server/internal/handler/followups.go.

/** Who filed the wake-up. Open on the wire like every other server enum. */
export type FollowupScheduledByType = "member" | "agent" | (string & {});

export interface Followup {
  id: string;
  issue_id: string;
  agent_id: string;
  agent_name: string;
  /** RFC 3339 instant the agent wakes up at. */
  fires_at: string;
  note: string;
  scheduled_by_type: FollowupScheduledByType;
  scheduled_by_id?: string | null;
  created_at: string;
}

/** Per-day caps from workspace.settings.followups. */
export interface FollowupBudget {
  max_per_agent_per_day: number;
  max_per_workspace_per_day: number;
}

export interface IssueFollowupsResponse {
  /** Pending follow-ups, soonest first. */
  followups: Followup[];
  budget: FollowupBudget;
}

export interface ScheduleFollowupInput {
  /** RFC 3339, or "+<minutes>". 1 minute to 30 days ahead. */
  when: string;
  /** At most 500 characters; the server defaults it. */
  note?: string;
  /** Required unless the issue is assigned to an agent. */
  agent_id?: string;
}
