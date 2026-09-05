import type { AcceptanceCriterion, Issue, IssueDecision, PlanVerification } from "./issue";
import type { MergeReadiness } from "./github";

// Review cockpit (K16): one read for the reviewer of an agent's work.

export interface ReviewCockpitRun {
  id: string;
  status: string;
  agent_id: string;
  created_at: string;
  started_at: string | null;
  completed_at: string | null;
  error: string | null;
  failure_reason?: string;
  handoff_note?: string;
}

export interface ReviewCockpitUsage {
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  /** 1e-10 USD; null when no usage row carried a price. */
  cost_usd_ticks: number | null;
  /** True when some usage rows carry no price: the cost is a floor. */
  uncosted: boolean;
}

export interface ReviewCockpit {
  issue: Issue;
  run: ReviewCockpitRun | null;
  runs: ReviewCockpitRun[];
  merge_readiness: MergeReadiness | null;
  usage: ReviewCockpitUsage | null;
  open_questions: IssueDecision[];
  criteria: AcceptanceCriterion[];
  plan_verification: PlanVerification | null;
  /** Reserved for K15's cross-provider verdict. */
  self_review: unknown;
  failed_sections: string[];
}
