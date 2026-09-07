import {
  deriveUsageBudgetLevel,
  type UsageBudgetLevel,
} from "@multica/core/billing";
import type {
  AutopilotQuotaUsage,
  IssueLimitUsage,
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
      level: UsageBudgetLevel;
      resetAt: string;
    };

export type IssueUsageView =
  | { kind: "unlimited" }
  | { kind: "unavailable" }
  | {
      kind: "metered";
      used: number;
      limit: number;
      progress: number;
      level: UsageBudgetLevel;
    };

/**
 * Quota admission counts completed and reserved runs. Keep reserved work
 * visible so the progress bar matches the server's blocking decision for a
 * limited workspace. Limit mode and the reached decision are server facts;
 * this helper only prepares presentation values and the alert/blocked level.
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
        level: deriveUsageBudgetLevel({ used, reserved, limit, reached }),
        resetAt,
      };
    }
  }

  return { kind: "unavailable" };
}

/**
 * Issue-count ceiling presentation. The server refuses new issues at the
 * Multica create boundary; this only maps usage into ok / alert / blocked.
 */
export function resolveIssueUsage(
  entitlements: WorkspaceSubscriptionEntitlements,
  usage: IssueLimitUsage | null | undefined,
  failed: boolean,
): IssueUsageView {
  if (entitlements.limits.issueCount.mode === "unlimited") {
    return { kind: "unlimited" };
  }

  const limit = entitlements.limits.issueCount.limit;
  if (
    failed ||
    usage == null ||
    limit == null ||
    usage.limit !== limit ||
    !Number.isFinite(usage.used) ||
    usage.used < 0
  ) {
    return { kind: "unavailable" };
  }

  const progress =
    limit === 0 ? 100 : Math.min(100, Math.max(0, (usage.used / limit) * 100));

  return {
    kind: "metered",
    used: usage.used,
    limit,
    progress,
    level: deriveUsageBudgetLevel({
      used: usage.used,
      limit,
      reached: usage.used >= limit,
    }),
  };
}

export function hasActiveWorkspaceSeatCapacity(
  summary: WorkspaceSubscriptionSummary | null | undefined,
): boolean {
  return summary?.seatCapacity != null;
}
