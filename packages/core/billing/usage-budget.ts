// @vitest-environment node
/**
 * Shared presentation of metered entitlement usage: ok → alert → blocked.
 *
 * "Blocked" matches Multica-controlled admission (create issue, reserve an
 * autopilot run, seat capacity). It does not claim to stop an already-running
 * provider CLI or external API spend outside Multica's queue.
 */

export type UsageBudgetLevel = "ok" | "alert" | "blocked";

/** Fraction of the limit at which the UI should warn before hard refusal. */
export const DEFAULT_USAGE_BUDGET_ALERT_RATIO = 0.8;

export interface UsageBudgetInput {
  /** Completed or confirmed consumption toward the limit. */
  used: number;
  /** In-flight holds that also count toward admission (e.g. reserved runs). */
  reserved?: number;
  /** Effective limit from the server entitlement. */
  limit: number;
  /**
   * When the server already decided capacity is exhausted, prefer that fact
   * over recomputing from used/reserved (avoids drift on observe/unavailable).
   */
  reached?: boolean | null;
  /** Override the default alert ratio; must be in (0, 1]. */
  alertRatio?: number;
}

/**
 * Derive the budget level for a metered Multica gate.
 *
 * - `blocked`: at or over the limit (or server `reached`), or a zero limit.
 * - `alert`: at or above the alert ratio but still below the hard limit.
 * - `ok`: below the alert ratio.
 */
export function deriveUsageBudgetLevel(
  input: UsageBudgetInput,
): UsageBudgetLevel {
  const used = input.used;
  const reserved = input.reserved ?? 0;
  const limit = input.limit;

  if (
    !Number.isFinite(used) ||
    !Number.isFinite(reserved) ||
    !Number.isFinite(limit) ||
    used < 0 ||
    reserved < 0 ||
    limit < 0
  ) {
    return "ok";
  }

  if (input.reached === true || limit === 0) {
    return "blocked";
  }

  const total = used + reserved;
  if (total >= limit) {
    return "blocked";
  }

  const ratio =
    typeof input.alertRatio === "number" &&
    Number.isFinite(input.alertRatio) &&
    input.alertRatio > 0 &&
    input.alertRatio <= 1
      ? input.alertRatio
      : DEFAULT_USAGE_BUDGET_ALERT_RATIO;

  if (total / limit >= ratio) {
    return "alert";
  }

  return "ok";
}
