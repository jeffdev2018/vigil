import type {
  AutopilotQuotaUsage,
  WorkspaceSubscriptionEntitlements,
  WorkspaceSubscriptionSummary,
} from "@multica/core/types";

export type AutopilotUsageView =
  | { kind: "unlimited" }
  | { kind: "unavailable" }
  | {
      kind: "metered";
      used: number;
      reserved: number;
      total: number;
      limit: number;
      progress: number;
      reached: boolean;
      /**
       * True once usage crosses the alert ratio but before the server refuses.
       * Derived here, not a server fact: it only changes how the same numbers
       * are labelled, never whether a run is admitted.
       */
      approaching: boolean;
      resetAt: string;
    };

/**
 * Fraction of the quota at which the UI warns before the hard refusal.
 *
 * "Reached" means Multica-controlled admission is closed (reserving an
 * autopilot run). It does not claim to stop provider spend happening outside
 * Multica's queue.
 */
export const AUTOPILOT_QUOTA_ALERT_RATIO = 0.8;

/**
 * Quota admission counts completed and reserved runs. Keep reserved work
 * visible so the progress bar matches the server's blocking decision for a
 * limited workspace. Limit mode and the reached decision are server facts;
 * this helper only prepares presentation values.
 */
export function resolveAutopilotUsage(
  entitlements: WorkspaceSubscriptionEntitlements,
  usage: AutopilotQuotaUsage | undefined,
  failed: boolean,
): AutopilotUsageView {
  if (entitlements.limits.autopilotRuns.mode === "unlimited") {
    return { kind: "unlimited" };
  }

  if (!failed && usage !== undefined && usage.action !== "off") {
    const { used, reserved, total, limit, reached, reset_at: resetAt } = usage;
    if (
      used !== null &&
      reserved !== null &&
      total !== null &&
      limit !== null &&
      reached !== null &&
      resetAt !== null &&
      used >= 0 &&
      reserved >= 0 &&
      limit >= 0 &&
      Number.isFinite(used) &&
      Number.isFinite(reserved) &&
      Number.isFinite(total) &&
      Number.isFinite(limit)
    ) {
      const progress =
        limit === 0
          ? 100
          : Math.min(100, Math.max(0, (total / limit) * 100));

      return {
        kind: "metered",
        used,
        reserved,
        total,
        limit,
        progress,
        reached,
        approaching:
          !reached && limit > 0 && total / limit >= AUTOPILOT_QUOTA_ALERT_RATIO,
        resetAt,
      };
    }
  }

  return { kind: "unavailable" };
}

export function hasActiveWorkspaceSeatCapacity(
  summary: WorkspaceSubscriptionSummary | null | undefined,
): boolean {
  return summary?.seatCapacity != null;
}
