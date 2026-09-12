"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { BarChart3, TrendingDown, TrendingUp } from "lucide-react";
import { BarChart, Bar, XAxis, YAxis, CartesianGrid } from "recharts";
import {
  dashboardVelocityWeeklyOptions,
  mergeThroughputWeeks,
  formatCycleTimeTrend,
} from "@multica/core/dashboard";
import type { DashboardVelocityWeekly } from "@multica/core/types";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@multica/ui/components/ui/chart";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { NumberFlow } from "@multica/ui/components/ui/number-flow";
import { cn } from "@multica/ui/lib/utils";
import { KpiCard } from "../../runtimes/components/shared";
import { formatShortDate, formatUsd } from "../../runtimes/utils";
import { labelOf } from "../../runtimes/components/charts/chart-label";
import { useLocale, useT } from "../../i18n";

// Member/agent colours follow the cycle capacity-bar convention: members are
// the primary brand series, agents the accent (chart-3) one.
function useVelocityChartConfig(): ChartConfig {
  const { t } = useT("usage");
  return useMemo(
    () => ({
      member: { label: t(($) => $.velocity.legend_member), color: "var(--chart-1)" },
      agent: { label: t(($) => $.velocity.legend_agent), color: "var(--chart-3)" },
    }),
    [t],
  );
}

function useCostChartConfig(): ChartConfig {
  const { t } = useT("usage");
  return useMemo(
    () => ({
      cost: { label: t(($) => $.velocity.cost_series), color: "var(--chart-2)" },
    }),
    [t],
  );
}

/**
 * Mixed member/agent velocity (JEF-251): how fast each pool closes issues,
 * how much each closes per week, and what an agent-closed issue costs.
 *
 * Cycle time reads better when it goes DOWN, so the trend chips invert the
 * cost-card colour rule: a positive delta (slower) is warning, a negative one
 * is success — same convention as CostPerDeliverableCard's trend.
 */
export function VelocityTab({
  wsId,
  days,
  projectId,
  locales,
}: {
  wsId: string;
  days: number;
  projectId: string | null;
  locales: string;
}) {
  const { t } = useT("usage");
  const { data, isLoading, isError, refetch } = useQuery(
    dashboardVelocityWeeklyOptions(wsId, days, projectId),
  );

  if (isError) {
    return (
      <div
        data-testid="velocity-error"
        role="alert"
        className="flex items-center justify-between gap-2 rounded-lg border bg-card px-4 py-2 text-caption text-destructive"
      >
        <span>{t(($) => $.velocity.load_error)}</span>
        <Button variant="outline" size="sm" onClick={() => void refetch()}>
          {t(($) => $.velocity.retry)}
        </Button>
      </div>
    );
  }
  if (isLoading || !data) {
    return (
      <div className="space-y-5" data-testid="velocity-loading">
        <Skeleton className="h-28 rounded-lg" />
        <Skeleton className="h-56 rounded-lg" />
        <Skeleton className="h-48 rounded-lg" />
      </div>
    );
  }
  // Defensive on shape: an older backend (or a test fixture) may hand back
  // something that is not this response.
  if (!data.cycle_time || !Array.isArray(data.throughput)) return null;

  const ct = data.cycle_time;
  const totalClosed = ct.member_count + ct.agent_count;
  const hasThroughput = data.throughput.some(
    (w) => w.member_count > 0 || w.agent_count > 0,
  );
  const hasCost = data.cost_per_closed_issue.length > 0;

  if (totalClosed === 0 && !hasThroughput && !hasCost) {
    return (
      <div
        data-testid="velocity-tab"
        data-empty="true"
        className="flex flex-col items-center rounded-lg border border-dashed py-12 text-center"
      >
        <BarChart3 className="h-6 w-6 text-faint-foreground" />
        <p className="mt-3 text-body font-medium">{t(($) => $.velocity.empty_title)}</p>
        <p className="mt-1 max-w-md text-caption text-muted-foreground">
          {t(($) => $.velocity.empty_body, { days })}
        </p>
      </div>
    );
  }

  return (
    <div data-testid="velocity-tab" className="space-y-5">
      <div className="grid grid-cols-1 divide-y rounded-lg border bg-card sm:grid-cols-3 sm:divide-x sm:divide-y-0">
        <CycleTimeKpi
          label={t(($) => $.velocity.kpi_member_cycle, { days })}
          medianDays={ct.member_median_days}
          prevMedianDays={ct.prev_member_median_days}
          count={ct.member_count}
        />
        <CycleTimeKpi
          label={t(($) => $.velocity.kpi_agent_cycle, { days })}
          medianDays={ct.agent_median_days}
          prevMedianDays={ct.prev_agent_median_days}
          count={ct.agent_count}
        />
        <KpiCard
          label={t(($) => $.velocity.kpi_throughput, { days })}
          value={
            <NumberFlow
              value={totalClosed}
              locales={locales}
              format={{ maximumFractionDigits: 0 }}
              aria-label={String(totalClosed)}
            />
          }
          hint={t(($) => $.velocity.throughput_hint, {
            member: ct.member_count,
            agent: ct.agent_count,
          })}
        />
      </div>

      <ThroughputChart weeks={data.throughput} days={days} />
      <CostPerIssueChart rows={data.cost_per_closed_issue} />
    </div>
  );
}

function CycleTimeKpi({
  label,
  medianDays,
  prevMedianDays,
  count,
}: {
  label: string;
  medianDays: number | null;
  prevMedianDays: number | null;
  count: number;
}) {
  const { t } = useT("usage");
  if (medianDays === null) {
    return (
      <KpiCard
        label={label}
        value={<span className="text-muted-foreground">—</span>}
        hint={t(($) => $.velocity.no_closed)}
      />
    );
  }
  const trend = formatCycleTimeTrend(medianDays, prevMedianDays);
  return (
    <KpiCard
      label={label}
      value={
        <span className="inline-flex items-baseline gap-1">
          {medianDays.toFixed(1)}
          <span className="text-body font-normal text-muted-foreground">
            {t(($) => $.velocity.days_unit)}
          </span>
        </span>
      }
      hint={
        <span className="inline-flex flex-wrap items-center gap-x-2">
          <span>{t(($) => $.velocity.closed_hint, { count })}</span>
          {trend !== null && (
            <span
              data-testid="velocity-trend"
              className={cn(
                "inline-flex items-center gap-0.5 tabular-nums",
                trend > 0 ? "text-warning" : "text-success",
              )}
            >
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

function ThroughputChart({
  weeks,
  days,
}: {
  weeks: DashboardVelocityWeekly["throughput"];
  days: number;
}) {
  const { t } = useT("usage");
  const locale = useLocale();
  const config = useVelocityChartConfig();
  const data = useMemo(
    () =>
      mergeThroughputWeeks(weeks, days).map((w) => ({
        weekStart: w.week_start,
        label: formatShortDate(w.week_start),
        member: w.member_count,
        agent: w.agent_count,
      })),
    [weeks, days],
  );
  const isEmpty = data.every((w) => w.member === 0 && w.agent === 0);

  return (
    <div className="rounded-lg border bg-card p-4" data-testid="velocity-throughput">
      <h4 className="mb-3 text-body font-semibold">
        {t(($) => $.velocity.throughput_title)}
      </h4>
      <div className="min-h-[240px]">
        {isEmpty ? (
          <div className="flex aspect-[3/1] flex-col items-center justify-center gap-2 rounded-md border border-dashed bg-muted/20 p-6 text-center">
            <BarChart3 className="h-5 w-5 text-faint-foreground" />
            <p className="text-caption text-muted-foreground">
              {t(($) => $.velocity.throughput_empty)}
            </p>
          </div>
        ) : (
          <ChartContainer config={config} className="aspect-[3/1] w-full">
            <BarChart data={data} margin={{ left: 0, right: 0, top: 4, bottom: 0 }}>
              <CartesianGrid vertical={false} />
              <XAxis
                dataKey="label"
                tickLine={false}
                axisLine={false}
                tickMargin={8}
                interval="preserveStartEnd"
              />
              <YAxis
                tickLine={false}
                axisLine={false}
                tickMargin={8}
                allowDecimals={false}
                width="auto"
              />
              <ChartTooltip
                content={
                  <ChartTooltipContent
                    formatter={(value, name) => `${value} ${labelOf(config, name)}`}
                    footer={(payload) => {
                      const total = payload.reduce(
                        (sum, item) =>
                          sum + (typeof item.value === "number" ? item.value : 0),
                        0,
                      );
                      return (
                        <div className="flex items-center justify-between gap-2 font-medium">
                          <span>{t(($) => $.velocity.tooltip_total)}</span>
                          <span className="font-mono tabular-nums">
                            {total.toLocaleString(locale)}
                          </span>
                        </div>
                      );
                    }}
                  />
                }
              />
              <Bar dataKey="member" stackId="throughput" fill="var(--color-member)" radius={[0, 0, 0, 0]} />
              <Bar dataKey="agent" stackId="throughput" fill="var(--color-agent)" radius={[3, 3, 0, 0]} />
            </BarChart>
          </ChartContainer>
        )}
      </div>
    </div>
  );
}

function CostPerIssueChart({
  rows,
}: {
  rows: DashboardVelocityWeekly["cost_per_closed_issue"];
}) {
  const { t } = useT("usage");
  const config = useCostChartConfig();
  const data = useMemo(
    () =>
      rows.map((w) => ({
        weekStart: w.week_start,
        label: formatShortDate(w.week_start),
        cost: w.mean_cost_usd_ticks / 1e10,
        count: w.issue_count,
      })),
    [rows],
  );

  return (
    <div className="rounded-lg border bg-card p-4" data-testid="velocity-cost">
      <h4 className="text-body font-semibold">{t(($) => $.velocity.cost_title)}</h4>
      <p className="mb-3 mt-0.5 text-caption text-muted-foreground">
        {t(($) => $.velocity.cost_caption)}
      </p>
      <div className="min-h-[240px]">
        {data.length === 0 ? (
          <div className="flex aspect-[3/1] flex-col items-center justify-center gap-2 rounded-md border border-dashed bg-muted/20 p-6 text-center">
            <BarChart3 className="h-5 w-5 text-faint-foreground" />
            <p className="text-caption text-muted-foreground">
              {t(($) => $.velocity.cost_empty)}
            </p>
          </div>
        ) : (
          <ChartContainer config={config} className="aspect-[3/1] w-full">
            <BarChart data={data} margin={{ left: 0, right: 0, top: 4, bottom: 0 }}>
              <CartesianGrid vertical={false} />
              <XAxis
                dataKey="label"
                tickLine={false}
                axisLine={false}
                tickMargin={8}
                interval="preserveStartEnd"
              />
              <YAxis
                tickLine={false}
                axisLine={false}
                tickMargin={8}
                tickFormatter={(v: number) => formatUsd(v)}
                width="auto"
              />
              <ChartTooltip
                content={
                  <ChartTooltipContent
                    formatter={(value, name) =>
                      typeof value === "number"
                        ? `${formatUsd(value)} ${labelOf(config, name)}`
                        : `${value} ${labelOf(config, name)}`
                    }
                  />
                }
              />
              <Bar dataKey="cost" fill="var(--color-cost)" radius={[3, 3, 0, 0]} />
            </BarChart>
          </ChartContainer>
        )}
      </div>
    </div>
  );
}
