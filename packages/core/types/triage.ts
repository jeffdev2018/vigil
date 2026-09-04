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
  title: string;
  body_markdown: string;
  payload: TriageItemPayload;
  /** Loose on purpose: a state added server-side must not fail the parse. */
  state: TriageItemState | (string & {});
  collapse_count: number;
  drop_reason?: string;
  issue_id?: string;
  duplicate_of_issue_id?: string;
  first_seen_at: string;
  resolved_at?: string | null;
  revision: number;
}

/** One inbound source and its 24h activity. */
export interface TriageSource {
  id: string;
  kind: string;
  ref_id: string;
  name: string;
  /** Loose on purpose: a mode added server-side must not fail the parse. */
  mode: TriageSourceMode | (string & {});
  items_24h: number;
  dropped_24h: number;
}

/** Queue volume summary for a workspace. */
export interface TriageStats {
  pending: number;
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
