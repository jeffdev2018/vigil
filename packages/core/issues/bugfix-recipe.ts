/**
 * Readiness for the guided bug → reproduction → fix → test → PR → delivery
 * review recipe. Pure — no I/O — so docs, UI, and tests share one definition.
 * Passing readiness does not start a provider agent; the fixture proof path
 * runs local tests only.
 */

export type BugFixRecipeStepId =
  | "runtime_online"
  | "cli_authenticated_or_na"
  | "repo_accessible"
  | "agent_assignable"
  | "delivery_criteria_ready"
  | "human_reviewer_present";

export type BugFixRecipeStepStatus =
  | "ready"
  | "blocked"
  | "not_applicable"
  | "unknown";

export interface BugFixRecipeStep {
  id: BugFixRecipeStepId;
  status: BugFixRecipeStepStatus;
}

export interface BugFixRecipeReadinessInput {
  /** At least one online runtime bound to the target agent. */
  hasOnlineRuntime: boolean;
  /**
   * Provider CLI auth for Claude/Codex when those providers are in play.
   * `not_applicable` when the agent uses a provider without Multica CLI auth.
   */
  cliAuth: "ready" | "blocked" | "not_applicable" | "unknown";
  /** Workspace (or project) has a linked local/git repo the daemon can open. */
  hasAccessibleRepo: boolean;
  /** Operator may invoke the agent that will own the issue (private-agent gate). */
  canInvokeAgent: boolean;
  /** Issue has at least one non-empty delivery criterion. */
  hasDeliveryCriteria: boolean;
  /** A human workspace member can perform delivery review. */
  hasHumanReviewer: boolean;
}

export interface BugFixRecipeReadiness {
  steps: BugFixRecipeStep[];
  /** Every required step is ready or not_applicable. */
  readyToStart: boolean;
  blockedCount: number;
}

const STEP_ORDER: BugFixRecipeStepId[] = [
  "runtime_online",
  "cli_authenticated_or_na",
  "repo_accessible",
  "agent_assignable",
  "delivery_criteria_ready",
  "human_reviewer_present",
];

/**
 * Ordered recipe stages humans and agents follow. Stage ids are stable for
 * docs and tests; they are not Multica issue statuses.
 */
export const BUG_FIX_RECIPE_STAGES = [
  "reproduce",
  "fix",
  "test",
  "open_pr",
  "delivery_review",
] as const;

export type BugFixRecipeStage = (typeof BUG_FIX_RECIPE_STAGES)[number];

export function deriveBugFixRecipeReadiness(
  input: BugFixRecipeReadinessInput,
): BugFixRecipeReadiness {
  const byId: Record<BugFixRecipeStepId, BugFixRecipeStepStatus> = {
    runtime_online: input.hasOnlineRuntime ? "ready" : "blocked",
    cli_authenticated_or_na: input.cliAuth,
    repo_accessible: input.hasAccessibleRepo ? "ready" : "blocked",
    agent_assignable: input.canInvokeAgent ? "ready" : "blocked",
    delivery_criteria_ready: input.hasDeliveryCriteria ? "ready" : "blocked",
    human_reviewer_present: input.hasHumanReviewer ? "ready" : "blocked",
  };

  const steps = STEP_ORDER.map((id) => ({ id, status: byId[id] }));
  const blockedCount = steps.filter((step) => step.status === "blocked").length;
  const readyToStart = steps.every(
    (step) => step.status === "ready" || step.status === "not_applicable",
  );

  return { steps, readyToStart, blockedCount };
}

/**
 * Next stage after a completed one. Unknown/invalid current returns the first
 * stage so a fresh recipe always has a concrete entry point.
 */
export function nextBugFixRecipeStage(
  current: BugFixRecipeStage | null | undefined,
): BugFixRecipeStage {
  if (!current) return "reproduce";
  const index = BUG_FIX_RECIPE_STAGES.indexOf(current);
  if (index < 0 || index >= BUG_FIX_RECIPE_STAGES.length - 1) {
    return "delivery_review";
  }
  return BUG_FIX_RECIPE_STAGES[index + 1]!;
}
