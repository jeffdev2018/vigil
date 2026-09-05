import type { IssuePriority, IssueStatus } from "./issue";

export type InboxSeverity = "action_required" | "attention" | "info";

export type InboxItemType =
  | "issue_assigned"
  | "issue_subscribed"
  | "unassigned"
  | "assignee_changed"
  | "status_changed"
  | "priority_changed"
  | "start_date_changed"
  | "due_date_changed"
  | "new_comment"
  | "mentioned"
  | "review_requested"
  | "task_completed"
  | "task_failed"
  | "agent_blocked"
  | "agent_completed"
  | "reaction_added"
  | "quick_create_done"
  | "quick_create_failed"
  // Quick create whose outcome could not be verified. Distinct from
  // quick_create_failed because it must NOT be rendered with failure framing:
  // the issue may actually have been created.
  | "quick_create_unconfirmed"
  // System notifications are intentionally issue-less. Keep them in the
  // same Inbox model so read/archive/realtime behavior remains consistent.
  | "autopilot_paused"
  | "autopilot_quota_exceeded"
  | "decision_request"
  | "decision_escalated"
  | "ownership_suggested"
  | "morning_briefing"
  | "standup_question"
  | "weekly_retro"
  | "trust_promotion_suggested"
  | "budget_warning"
  | "budget_exceeded"
  // A postmortem was drafted after a failed run and waits for review.
  | "postmortem_ready"
  // The triage queue has items nobody has decided on for two days. Filed for
  // the workspace's admins/owners, at most once a day per workspace.
  | "triage_stale";

/**
 * One workspace's unread inbox count in the cross-workspace summary
 * (`GET /api/inbox/unread-summary`). The sidebar uses this to light a dot on
 * the workspace switcher when a workspace OTHER than the active one has
 * unread items.
 */
export interface InboxWorkspaceUnread {
  workspace_id: string;
  count: number;
}

export interface InboxItem {
  id: string;
  workspace_id: string;
  recipient_type: "member" | "agent";
  recipient_id: string;
  actor_type: "member" | "agent" | "system" | null;
  actor_id: string | null;
  type: InboxItemType;
  severity: InboxSeverity;
  issue_id: string | null;
  title: string;
  body: string | null;
  issue_status: IssueStatus | null;
  /**
   * Current priority of the linked issue. Optional so an installed Desktop
   * client remains compatible with an older backend that predates this Inbox
   * projection; null also covers notifications without a linked issue.
   */
  issue_priority?: IssuePriority | null;
  read: boolean;
  archived: boolean;
  created_at: string;
  details: Record<string, string> | null;
}

/** Attention Inbox (K02): an inbox item with its server-computed risk. */
export interface AttentionInboxItem extends InboxItem {
  risk_score: number;
  reason: string;
}

// Morning briefing (K30).
export interface BriefingItem {
  issue_id: string;
  identifier: string;
  title: string;
  status: string;
  reason?: string;
  pending_decisions?: number;
}

export interface MorningBriefing {
  // Multichannel digest (K64): the LLM narration and where the day's briefing went.
  narrative?: string;
  channels_delivered?: string[];
  date: string;
  merged: BriefingItem[];
  awaiting_review: BriefingItem[];
  blocked: BriefingItem[];
  sent_at: string | null;
  already_sent?: boolean;
}

// Standup and retro (K34).
export interface RetroRun {
  run_id: string;
  issue_id: string;
  identifier: string;
  title: string;
  status: string;
  agent_id: string;
  minutes: number;
  error?: string;
}

export interface RetroAgent {
  agent_id: string;
  name: string;
  runs_total: number;
  runs_failed: number;
  runs_accepted: number;
  runs_reopened: number;
  runs_no_intervention: number;
  cost_usd_ticks: number;
}

export interface WeeklyRetro {
  week_start: string;
  week_end: string;
  runs_total: number;
  runs_by_status: Record<string, number>;
  median_minutes: number;
  failed: RetroRun[];
  agents: RetroAgent[];
  skill_proposals: { text: string; source: string }[];
  narrative: string;
  generated_at: string | null;
}
