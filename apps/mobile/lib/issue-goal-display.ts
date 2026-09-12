/**
 * Issue goal-loop display helpers (goal_loop). Named `issue-goal-display`,
 * not `goal-display`, because `lib/goal-display.ts` already exists for the
 * unrelated K74 workspace-mission goal tree (Goal / GoalStatus) — same word,
 * different feature (see packages/core/types/issue-goal.ts's own naming
 * note: `IssueGoal*`, not `Goal*`).
 *
 * `status`, `question.kind` and `last_outcome` all stay free strings on the
 * wire (see ./data/schemas.ts IssueGoalSchema comment); every lookup here
 * falls back to the raw value instead of crashing or vanishing (root
 * CLAUDE.md "API Response Compatibility" — server-driven enum switches need
 * a default branch). Mobile is English-only today; web's namespace for this
 * feature will be `goal_loop` when it ships i18n strings.
 */
import type { IssueGoal } from "@/data/schemas";

export const ISSUE_GOAL_STATUS_LABEL: Record<string, string> = {
  active: "Active",
  paused: "Paused",
  waiting_user: "Waiting on you",
  satisfied: "Done",
  stopped: "Stopped",
};

export function issueGoalStatusLabel(status: string): string {
  return ISSUE_GOAL_STATUS_LABEL[status] ?? status;
}

// Wire values for last_outcome (server/pkg/goalstate, mirrored in
// packages/core/types/issue-goal.ts): "" | "satisfied" | "continued" |
// "stopped:<why>".
const STOPPED_REASON_LABEL: Record<string, string> = {
  exhausted: "ran out of continuations",
  stagnation: "made no further progress",
  needs_user_input: "needs your input",
  external_wait: "is waiting on something external",
  run_failed: "the run failed",
  judge_unavailable: "the judge was unavailable",
  paused: "was paused",
  issue_changed: "the issue changed",
};

/** null when there is nothing to show yet (no run has settled). */
export function issueGoalOutcomeLabel(lastOutcome: string): string | null {
  if (!lastOutcome) return null;
  if (lastOutcome === "satisfied") return "Goal satisfied";
  if (lastOutcome === "continued") return "Continued";
  if (lastOutcome.startsWith("stopped:")) {
    const reason = lastOutcome.slice("stopped:".length);
    return `Stopped — ${STOPPED_REASON_LABEL[reason] ?? reason}`;
  }
  // Unrecognised outcome string: show it verbatim rather than drop it.
  return lastOutcome;
}

export function issueGoalProgressLabel(
  goal: Pick<IssueGoal, "continuation" | "max_continuations">,
): string {
  return `Continuation ${goal.continuation} / ${goal.max_continuations}`;
}

export interface IssueGoalEvidencePreview {
  shown: string[];
  hiddenCount: number;
}

/** First `limit` evidence lines plus how many more are hidden. */
export function issueGoalEvidencePreview(
  goal: Pick<IssueGoal, "evidence">,
  limit = 3,
): IssueGoalEvidencePreview {
  return {
    shown: goal.evidence.slice(0, limit),
    hiddenCount: Math.max(0, goal.evidence.length - limit),
  };
}

// Set/edit-goal form bounds. Mirrors server/internal/service/goal_loop.go
// SetGoal (goalDescriptionCap=6000, goalMaxMaxContinuations=20). The server
// is still authoritative (it re-validates and 400s with the same numbers);
// this lets the form disable Save and show the same message before a
// round-trip instead of only after one.
export const ISSUE_GOAL_TEXT_MAX = 6000;
export const ISSUE_GOAL_MAX_CONTINUATIONS_MIN = 1;
export const ISSUE_GOAL_MAX_CONTINUATIONS_MAX = 20;

/** null when the form is submittable. */
export function issueGoalFormError(
  goal: string,
  maxContinuations: number,
): string | null {
  if (goal.trim().length === 0) return "Goal is required.";
  if (goal.length > ISSUE_GOAL_TEXT_MAX) {
    return `Goal is longer than ${ISSUE_GOAL_TEXT_MAX} characters.`;
  }
  if (
    !Number.isInteger(maxContinuations) ||
    maxContinuations < ISSUE_GOAL_MAX_CONTINUATIONS_MIN ||
    maxContinuations > ISSUE_GOAL_MAX_CONTINUATIONS_MAX
  ) {
    return `Max continuations must be between ${ISSUE_GOAL_MAX_CONTINUATIONS_MIN} and ${ISSUE_GOAL_MAX_CONTINUATIONS_MAX}.`;
  }
  return null;
}

/**
 * Server errors from this handler family come back as `{"error": "..."}`
 * (server/internal/handler/handler.go `writeError`), not `{"message": ...}`
 * — the field api.ts's generic error parsing reads. Read the specific text
 * off `ApiError.body` when present, same idiom as lib/dispatch-reason.ts.
 */
export function apiErrorMessage(e: unknown, fallback: string): string {
  const body = (e as { body?: unknown } | null)?.body;
  if (body && typeof body === "object") {
    const msg = (body as { error?: unknown }).error;
    if (typeof msg === "string" && msg) return msg;
  }
  if (e instanceof Error && e.message) return e.message;
  return fallback;
}

/** Mirrors packages/views goal_loop.blockers labels (mobile is English-only). */
export function issueGoalBlockerLabel(blocker: string): string {
  switch (blocker) {
    case "goal_not_met_yet":
      return "Goal not met yet";
    case "missing_evidence":
      return "Missing evidence";
    case "needs_user_input":
      return "Needs your input";
    case "external_wait":
      return "Waiting on something external";
    case "run_failed":
      return "The run failed";
    default:
      return blocker;
  }
}
