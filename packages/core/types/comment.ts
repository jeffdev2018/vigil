export type CommentType = "comment" | "status_change" | "progress_update" | "system";

// `system` is used by platform-generated rows (e.g. the parent-issue
// child-done notification, MUL-2538). System rows carry a zero UUID for
// author_id; render paths should branch on author_type rather than the UUID.
export type CommentAuthorType = "member" | "agent" | "system";

export interface Reaction {
  id: string;
  comment_id: string;
  actor_type: string;
  actor_id: string;
  emoji: string;
  created_at: string;
  comment_revision?: number;
}

/**
 * Where a comment thread is pinned in a pull request's diff (F07 / JEF-21).
 *
 * The anchor belongs to the thread ROOT; a reply carries the same object,
 * resolved by the server at read time. `kind` is an open string — a thread
 * whose kind this build does not know renders without its anchor rather than
 * disappearing from the timeline.
 */
export interface CommentAnchor {
  kind: string;
  pr_source: string;
  pr_id: string;
  /** The revision the line range belongs to. Part of the anchor's meaning. */
  head_sha: string;
  file_path: string;
  line_start: number;
  line_end: number;
  /** "new" is the changed code; "old" points at a deletion. */
  side: string;
  review_flag_id?: string | null;
}

/**
 * What the client sends to pin a NEW thread. `head_sha` is optional: omitted,
 * the server uses the pull request's current head, which is what the reviewer
 * is looking at when they click a line.
 */
export interface CreateCommentAnchor {
  pr_id: string;
  file_path: string;
  line_start: number;
  line_end?: number;
  side?: string;
  head_sha?: string;
  review_flag_id?: string;
}

/** One anchored discussion: the root, its replies, and the shared anchor. */
export interface AnchoredThread {
  root: Comment;
  replies: Comment[];
  anchor?: CommentAnchor | null;
  anchor_stale: boolean;
}

export interface AnchoredThreads {
  threads: AnchoredThread[];
}

export interface Comment {
  id: string;
  issue_id: string;
  author_type: CommentAuthorType;
  author_id: string;
  content: string;
  type: CommentType;
  parent_id: string | null;
  reactions: Reaction[];
  attachments: import("./attachment").Attachment[];
  created_at: string;
  updated_at: string;
  /** Monotonic server revision; absent when connected to an older backend. */
  revision?: number;
  /** Parent issue revision after this semantic comment mutation. */
  issue_revision?: number;
  resolved_at: string | null;
  resolved_by_type: CommentAuthorType | null;
  resolved_by_id: string | null;
  source_task_id?: string | null;
  // The quick action that produced this comment (MUL-5465). A quick action
  // posts an ORDINARY comment and marks it with this id; the collapsed card
  // keys off the id rather than a dedicated `type`, because `type` is
  // client-supplied on the generic comment endpoint and would be forgeable.
  quick_action_id?: string | null;
  // Per-target result of every explicit @agent / @squad mention in this comment
  // (MUL-4525 §2). Present only on create/edit responses; older servers omit it.
  trigger_outcomes?: CommentTriggerOutcome[];
  // Diff anchor (F07). Absent on a backend that predates the feature, and null
  // on every ordinary comment. A reply carries its thread root's anchor.
  anchor?: CommentAnchor | null;
  /** The anchored head is no longer the pull request's head: code has moved. */
  anchor_stale?: boolean;
}

// The domain result of one explicitly-mentioned trigger target. Success-shaped
// statuses (queued/coalesced/deferred) mean the mention was handled; `blocked`
// means it was refused with an enumeration-safe reason_code.
export type CommentTriggerStatus =
  | "queued"
  | "coalesced"
  | "deferred"
  | "blocked";

export interface CommentTriggerOutcome {
  target_type: string; // "agent" | "squad"
  target_id: string;
  status: CommentTriggerStatus | string;
  reason_code: string;
}

export type CommentTriggerSource =
  | "issue_assignee"
  | "mention_agent"
  | "mention_squad_leader";

export interface CommentTriggerPreviewAgent {
  id: string;
  name: string;
  avatar_url?: string;
  source: CommentTriggerSource | string;
  reason: string;
}

export interface CommentTriggerPreview {
  agents: CommentTriggerPreviewAgent[];
  // Explicit @agent / @squad mentions that will NOT trigger if posted as-is
  // (MUL-4525 §2). Additive: older servers omit it.
  blocked?: CommentTriggerOutcome[];
}
