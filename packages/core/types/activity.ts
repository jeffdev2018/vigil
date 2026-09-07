import type { CommentAuthorType, Reaction } from "./comment";
import type { Attachment } from "./attachment";

export interface AssigneeFrequencyEntry {
  assignee_type: string;
  assignee_id: string;
  frequency: number;
}

export interface TimelineEntry {
  type: "activity" | "comment";
  id: string;
  actor_type: string;
  actor_id: string;
  created_at: string;
  /** Display identity hydrated from the actor's global user row when available. */
  actor_name?: string;
  actor_avatar_url?: string;
  // Activity fields
  action?: string;
  details?: Record<string, unknown>;
  // Comment fields
  content?: string;
  parent_id?: string | null;
  updated_at?: string;
  revision?: number;
  comment_type?: string;
  /** Set only on comments a quick action produced (MUL-5465). Unforgeable. */
  quick_action_id?: string | null;
  /**
   * Agent-to-agent message intent (F19): question | review | handoff. Written
   * only by POST /issues/{id}/agent-messages, so it cannot be forged through the
   * comment endpoint. Typed as a free string: an intent this build cannot label
   * renders as an ordinary comment.
   */
  a2a_intent?: string | null;
  reactions?: Reaction[];
  attachments?: Attachment[];
  resolved_at?: string | null;
  resolved_by_type?: CommentAuthorType | null;
  resolved_by_id?: string | null;
  source_task_id?: string | null;
  /** Set by frontend coalescing when consecutive identical activities are merged. */
  coalesced_count?: number;
  /**
   * Diff anchor of the thread this comment belongs to (F07). Present on the
   * root AND on every reply. Absent on activity rows and on unanchored
   * comments, so the timeline chip is drawn only when there is one.
   */
  anchor?: import("./comment").CommentAnchor | null;
  anchor_stale?: boolean;
}
