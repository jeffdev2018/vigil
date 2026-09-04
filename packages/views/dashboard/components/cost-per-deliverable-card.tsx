"use client";

import { useQuery } from "@tanstack/react-query";
import { TrendingDown, TrendingUp } from "lucide-react";
import { dashboardCostPerDeliverableOptions } from "@multica/core/dashboard/queries";
import type { DeliverableCostStats } from "@multica/core/types";
import { CurrencyNumberFlow } from "@multica/ui/components/ui/number-flow";
import { cn } from "@multica/ui/lib/utils";
import { KpiCard } from "../../runtimes/components/shared";
import { useT } from "../../i18n";

/**
 * Cost per deliverable (K04): the median cost of an issue closed and of a
 * pull request merged in the period, with the mean and the trend against
 * the previous period. Deliverables without a run are absent rather than
 * free; without any deliverable the card says so instead of showing zero.
 */
export function CostPerDeliverableCard({
  wsId,
  days,
  projectId,
  tz,
  locales,
}: {
  wsId: string;
  days: number;
  projectId: string | null;
  tz: string;
  locales: string;
}) {
  const { t } = useT("usage");
  const { data, isError } = useQuery(dashboardCostPerDeliverableOptions(wsId, days, projectId, tz));
  // Defensive on shape: an older backend (or a test fixture) may hand back
  // something that is not this response.
  if (!data || isError || !data.issues || !data.pull_requests) return null;
  if (data.issues.count === 0 && data.pull_requests.count === 0) {
    return (
      <div data-testid="cost-per-deliverable" data-empty="true" className="rounded-lg border bg-card px-4 py-3 text-caption text-muted-foreground">
        {t(($) => $.deliverable.empty, { days })}
      </div>
    );
  }
  return (
    <div data-testid="cost-per-deliverable" className="grid grid-cols-1 divide-y rounded-lg border bg-card sm:grid-cols-2 sm:divide-x sm:divide-y-0">
      <DeliverableKpi label={t(($) => $.deliverable.issue_label, { days })} stats={data.issues} locales={locales} />
      <DeliverableKpi label={t(($) => $.deliverable.pr_label, { days })} stats={data.pull_requests} locales={locales} />
    </div>
  );
}

function DeliverableKpi({ label, stats, locales }: { label: string; stats: DeliverableCostStats; locales: string }) {
  const { t } = useT("usage");
  if (stats.count === 0) {
    return <KpiCard label={label} value={<span className="text-muted-foreground">—</span>} hint={t(($) => $.deliverable.none)} />;
  }
  const trend = stats.trend_pct;
  return (
    <KpiCard
      label={label}
      value={<CurrencyNumberFlow value={stats.median_usd_ticks / 1e10} locales={locales} />}
      hint={
        <span className="inline-flex flex-wrap items-center gap-x-2">
          <span>
            {t(($) => $.deliverable.hint, { count: stats.count, mean: (stats.mean_usd_ticks / 1e10).toFixed(2) })}
            {stats.uncosted_count > 0 && ` · ${t(($) => $.deliverable.floor)}`}
          </span>
          {trend !== null && (
            <span data-testid="deliverable-trend" className={cn("inline-flex items-center gap-0.5 tabular-nums", trend > 0 ? "text-warning" : "text-success")}>
              {trend > 0 ? <TrendingUp className="size-3" /> : <TrendingDown className="size-3" />}
              {trend > 0 ? "+" : ""}
              {trend.toFixed(0)}%
            </span>
          )}
        </span>
      }
    />
  );
}
