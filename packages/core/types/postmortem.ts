/**
 * Lifecycle state of one postmortem. Drafts await a human decision; approve
 * keeps the artifact, discard drops it.
 */
export type PostmortemState = "draft" | "approved" | "discarded";

/** Why the postmortem was generated. */
export type PostmortemTrigger = "failed" | "costly";

/** One drafted postmortem as the review UI renders it. */
export interface Postmortem {
  id: string;
  source_task_id: string;
  issue_id?: string;
  agent_id?: string;
  /** Loose on purpose: a trigger added server-side must not fail the parse. */
  trigger: PostmortemTrigger | (string & {});
  /** Loose on purpose: a state added server-side must not fail the parse. */
  state: PostmortemState | (string & {});
  failure_reason: string;
  summary: string;
  root_cause: string;
  impact: string;
  preventive_rules: string[];
  /** Provider-reported run cost in 1e-10 USD; absent when none was reported. */
  cost_usd_ticks?: number;
  /** True when the assist-layer LLM drafted it, false for the scaffold. */
  llm_generated: boolean;
  resolved_at?: string | null;
  revision: number;
  /** Set on the approve response: preventive rules copied into agent memory. */
  applied_rules?: number;
  created_at: string;
}

/** Per-state counts that drive the nav badge and the page filter chips. */
export interface PostmortemStats {
  draft: number;
  approved: number;
  discarded: number;
}

export interface PostmortemsResponse {
  items: Postmortem[];
  next_cursor?: string;
}
