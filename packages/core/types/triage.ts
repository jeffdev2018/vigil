import type { Issue } from "./issue";

/**
 * Admission mode for one inbound source. `gate` parks deliveries as pending
 * triage items; `direct` creates issues immediately (today's behavior);
 * `blocked` refuses and records a drop.
 */
export type TriageSourceMode = "gate" | "direct" | "blocked";

/** Lifecycle state of one triage item. */
export type TriageItemState =
  | "pending"
  | "accepted"
  | "dismissed"
  | "merged";

/**
 * The stored capture payload: `size` is the original delivery size in bytes;
 * `body` carries the embedded trigger payload when the server kept it, and
 * `truncated` marks the stub written when the delivery was too large.
 */
export interface TriageItemPayload {
  size?: number;
  body?: Record<string, unknown>;
  truncated?: boolean;
}

/** One queue entry as the UI renders it. */
export interface TriageItem {
  id: string;
  source_id: string;
  source_name: string;
  source_kind: string;
  origin_type: string;
  /** The object the item came from: a meeting id for `origin_type: "meeting"`, an autopilot id for "autopilot". */
  origin_id?: string;
  title: string;
  body_markdown: string;
  payload: TriageItemPayload;
  /** Loose on purpose: a state added server-side must not fail the parse. */
  state: TriageItemState | (string & {});
  collapse_count: number;
  drop_reason?: string;
  /** Why the item left the queue: a human's reason, "rule: <title>", or "auto: N% confidence…". */
  resolution_reason?: string;
  /** "member" for a human decision, "system" for an automatic one. */
  resolved_by_type?: string;
  issue_id?: string;
  duplicate_of_issue_id?: string;
  /** Set while the item is parked by a snooze; cleared once it comes due. */
  snoozed_until?: string | null;
  /** An agent's suggestion (agents suggest, humans decide). Advisory only. */
  verdict?: TriageVerdict | (string & {});
  verdict_reason?: string;
  verdict_agent_id?: string;
  verdict_at?: string | null;
  first_seen_at: string;
  resolved_at?: string | null;
  revision: number;
}

/** What an agent may suggest on a pending item. Humans still decide. */
export type TriageVerdict = "accept" | "dismiss";

/** One inbound source: its admission policy, and what it did to the queue. */
export interface TriageSource {
  id: string;
  kind: string;
  ref_id: string;
  name: string;
  /** Loose on purpose: a mode added server-side must not fail the parse. */
  mode: TriageSourceMode | (string & {});
  /** Resolve this source's items into issues without a human. */
  auto_accept: boolean;
  /** Anti-flood ceiling; `0` means no cap. */
  cap_per_hour: number;
  /** How long an unresolved item survives; `0` means the default retention. */
  expiry_days: number;
  /** Items from this source still waiting on a human. */
  pending: number;
  items_24h: number;
  dropped_24h: number;
}

/** The subset of a source's policy a human can patch. */
export interface TriageSourcePatch {
  mode?: TriageSourceMode;
  auto_accept?: boolean;
  cap_per_hour?: number;
  expiry_days?: number;
}

/** Queue volume summary for a workspace. */
export interface TriageStats {
  pending: number;
  /** Pending items parked by a snooze — excluded from `pending`. */
  snoozed: number;
  shadow_pending: number;
  dropped_24h: number;
  oldest_pending_age_seconds: number;
  sources: TriageSource[];
}

export interface TriageItemsResponse {
  items: TriageItem[];
  next_cursor?: string;
}

/** One accept outcome inside a batch. */
export type TriageBatchOutcome =
  | "accepted"
  | "duplicate"
  | "limit_reached"
  | "not_found"
  | "not_pending"
  | "error";

export interface TriageBatchAcceptResult {
  id: string;
  /** Loose on purpose: an outcome added server-side must not fail the parse. */
  outcome: TriageBatchOutcome | (string & {});
  issue_id?: string;
  duplicate_of_issue_id?: string;
  duplicate_issue_identifier?: string;
}

export interface TriageBatchAcceptResponse {
  items: TriageBatchAcceptResult[];
  /** Present (as "issue_limit_reached") when the batch stopped early at quota. */
  stopped?: string;
}

export interface AcceptTriageItemResponse {
  item_id: string;
  state: string;
  /** The created issue. Present on a 200 from a current backend. */
  issue?: Issue;
}

export interface DismissTriageItemResponse {
  item_id: string;
  state: string;
}

export interface MergeTriageItemResponse {
  item_id: string;
  state: string;
  duplicate_of_issue_id: string;
  duplicate_issue_identifier: string;
}

export interface SnoozeTriageItemResponse {
  item_id: string;
  state: string;
  snoozed_until?: string | null;
}

/** What the human picked in "Accept as…" instead of inheriting the origin. */
export interface AcceptTriageItemOverrides {
  assignee_type?: "member" | "agent";
  assignee_id?: string;
  project_id?: string;
  priority?: string;
  labels?: string[];
}

export interface TriageBatchDismissResult {
  id: string;
  /** Loose on purpose: an outcome added server-side must not fail the parse. */
  outcome: "dismissed" | "not_found" | "not_pending" | "error" | (string & {});
}

export interface TriageBatchDismissResponse {
  items: TriageBatchDismissResult[];
}

// Triage auto-ML (K61).
export interface TriageNeighbor {
  id: string;
  title: string;
  state: string;
  score: number;
}

export interface TriageSuggestion {
  item_id: string;
  ready: boolean;
  examples: number;
  min_examples: number;
  suggested?: "accept" | "dismiss" | (string & {});
  confidence: number;
  neighbors: TriageNeighbor[];
}

export interface TriageAutoSettings {
  enabled: boolean;
  threshold: number;
  min_examples: number;
}

export interface TriageSuggestionsResponse {
  suggestions: Record<string, TriageSuggestion>;
  auto: TriageAutoSettings;
}

/**
 * The workspace's email intake endpoint, returned when it is created or its
 * token is rotated. `token` is present exactly once, in that response: the
 * server stores only its digest, so a lost token is rotated, never recovered.
 * `url` is empty when the deployment has no public URL configured; `path` is
 * always usable behind whatever host the operator serves.
 */
export interface TriageEmailSource {
  id: string;
  mode: TriageSourceMode | (string & {});
  path: string;
  url?: string;
  token: string;
}
