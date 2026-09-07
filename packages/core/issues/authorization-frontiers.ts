/**
 * Soft board/delivery signals vs Multica-enforced hard gates.
 *
 * Product rule (product-priorities): `in_review`, delivery Accept, and Done are
 * not merge/deploy authorization. Real prohibitions live on controlled
 * Multica frontiers (invoke permission, membership, run-scoped tokens, CAS).
 * A prompt or status label alone is never a security boundary.
 */

/** Human-facing signals that must never be treated as action authorization. */
export const SOFT_AUTHORIZATION_SIGNALS = [
  "board_status_in_review",
  "board_status_done",
  "delivery_acceptance",
  "delivery_changes_requested",
  "coding_tool_approval_prompts",
] as const;

export type SoftAuthorizationSignal =
  (typeof SOFT_AUTHORIZATION_SIGNALS)[number];

/**
 * Frontiers where Multica actually refuses or scopes an action. External
 * merge/deploy remains outside this list — those stay with the VCS host and
 * the credentials on the daemon user.
 */
export const HARD_AUTHORIZATION_GATES = [
  "workspace_membership",
  "private_agent_invoke",
  "run_scoped_multica_token",
  "delivery_review_cas",
  "decision_destinee_only",
  "cli_auth_operator_only",
  "memory_evaluation_human_adopt",
] as const;

export type HardAuthorizationGate = (typeof HARD_AUTHORIZATION_GATES)[number];

export type AuthorizationKind = "soft_signal" | "hard_gate" | "unknown";

const SOFT = new Set<string>(SOFT_AUTHORIZATION_SIGNALS);
const HARD = new Set<string>(HARD_AUTHORIZATION_GATES);

export function authorizationKind(id: string): AuthorizationKind {
  if (SOFT.has(id)) return "soft_signal";
  if (HARD.has(id)) return "hard_gate";
  return "unknown";
}

/**
 * True when a board/delivery signal must not be read as merge, deploy, or
 * tool-approval authority. Unknown ids stay false so callers cannot invent
 * a soft signal by typo.
 */
export function isSoftAuthorizationSignal(id: string): boolean {
  return authorizationKind(id) === "soft_signal";
}

/**
 * True when Multica itself is the enforcer for this frontier. Unknown ids
 * stay false — claiming a hard gate requires an explicit registry entry.
 */
export function isHardAuthorizationGate(id: string): boolean {
  return authorizationKind(id) === "hard_gate";
}

/**
 * Board `in_review` (and same-category customs) finalizes Multica autopilot
 * bookkeeping and archives run-failure notifications. That is still not
 * delivery acceptance and not merge/deploy permission.
 */
export const BOARD_IN_REVIEW_MULTICA_SIDE_EFFECTS = [
  "finalize_autopilot_run",
  "archive_run_failure_notifications",
] as const;
