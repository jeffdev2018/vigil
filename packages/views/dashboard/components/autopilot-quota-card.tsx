"use client";

import { useQuery } from "@tanstack/react-query";
import { Zap } from "lucide-react";
import { autopilotQuotaUsageOptions } from "@multica/core/autopilots";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

// Past this share of the quota the card stops being informational and starts
// being a warning: at 80% a team on a weekly cadence has about one period left
// before autopilots stop firing.
const WARN_AT = 0.8;

/**
 * Autopilot quota (K68): how many autopilot runs this billing period has left.
 *
 * The number already exists behind GET /api/autopilots/usage but was only read
 * by the billing tab, so the people who actually watch autopilots had no way
 * to see it without opening workspace settings. This is a read-only mirror of
 * that query — the same cache entry, no second request.
 *
 * Renders nothing when the workspace has no quota to report: quota enforcement
 * off, an unlimited plan, or a backend that does not answer.
 */
export function AutopilotQuotaCard({ wsId }: { wsId: string }) {
  const { t } = useT("usage");
  const { data } = useQuery(autopilotQuotaUsageOptions(wsId));

  const limit = data?.limit ?? null;
  if (!data || data.action === "off" || limit === null || limit <= 0) return null;

  // `total` counts reserved runs alongside used ones, so it is what the quota
  // actually charges against; fall back to `used` on a backend that omits it.
  const used = data.total ?? data.used ?? 0;
  const ratio = used / limit;
  const reached = data.reached === true || used >= limit;
  const warning = !reached && ratio >= WARN_AT;

  return (
    <div
      data-testid="autopilot-quota"
      data-state={reached ? "reached" : warning ? "warning" : "ok"}
      className="flex items-center gap-3 rounded-lg border bg-card px-4 py-3"
    >
      <Zap
        aria-hidden="true"
        className={cn(
          "size-4 shrink-0",
          reached || warning ? "text-warning" : "text-muted-foreground",
        )}
      />
      <div className="min-w-0 flex-1">
        <div className="text-caption font-medium">
          {t(($) => $.autopilot_quota.title, { used, limit })}
        </div>
        {(reached || warning) && (
          <div className="mt-0.5 text-caption text-warning">
            {reached
              ? t(($) => $.autopilot_quota.reached)
              : t(($) => $.autopilot_quota.near_limit)}
          </div>
        )}
      </div>
    </div>
  );
}
