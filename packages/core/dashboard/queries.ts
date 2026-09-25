import { keepPreviousData, queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { DashboardThroughputWeek } from "../types";

export const dashboardKeys = {
  all: (wsId: string) => ["dashboard", wsId] as const,
  daily: (
    wsId: string,
    days: number,
    projectId: string | null,
    tz: string,
  ) => [...dashboardKeys.all(wsId), "daily", days, projectId, tz] as const,
  byAgent: (
    wsId: string,
    days: number,
    projectId: string | null,
    tz: string,
  ) => [...dashboardKeys.all(wsId), "by-agent", days, projectId, tz] as const,
  agentRuntime: (
    wsId: string,
    days: number,
    projectId: string | null,
    tz: string,
  ) => [...dashboardKeys.all(wsId), "agent-runtime", days, projectId, tz] as const,
  runTimeDaily: (
    wsId: string,
    days: number,
    projectId: string | null,
    tz: string,
  ) => [...dashboardKeys.all(wsId), "runtime-daily", days, projectId, tz] as const,
  failuresDaily: (
    wsId: string,
    days: number,
    projectId: string | null,
    tz: string,
  ) => [...dashboardKeys.all(wsId), "failures-daily", days, projectId, tz] as const,
  failuresByAgent: (
    wsId: string,
    days: number,
    projectId: string | null,
    tz: string,
  ) =>
    [...dashboardKeys.all(wsId), "failures-by-agent", days, projectId, tz] as const,
  routingStats: (wsId: string) =>
    [...dashboardKeys.all(wsId), "routing-stats"] as const,
  workflowStats: (wsId: string) =>
    [...dashboardKeys.all(wsId), "workflow-stats"] as const,
  velocityWeekly: (
    wsId: string,
    days: number,
    projectId: string | null,
  ) => [...dashboardKeys.all(wsId), "velocity-weekly", days, projectId] as const,
};

// The server materializes these rollups on a 5-minute cadence, so a mounted
// dashboard re-polls on that same cadence — polling faster would only re-read
// an unchanged rollup. The short staleTime keeps re-entering the page honest:
// anything older than a minute refetches on mount instead of waiting out the
// interval. Neither fires for unmounted queries or backgrounded windows.
const STALE_TIME = 60 * 1000;
const REFETCH_INTERVAL = 5 * 60 * 1000;

// Range changes should keep the previous result mounted so KPI cards and
// charts transition in place instead of falling back to a full-page skeleton.
// Scope changes are deliberately excluded: carrying data across workspaces,
// projects, report kinds, or timezones would briefly display the wrong data.
function isSameDashboardScope(
  previousKey: readonly unknown[] | undefined,
  nextKey: readonly unknown[],
): boolean {
  if (!previousKey || previousKey.length !== nextKey.length) return false;
  return previousKey.every(
    (part, index) => index === 3 || Object.is(part, nextKey[index]),
  );
}

// `tz` participates in every dashboard key so a Preferences change
// repoints the cache. Every series — token rollups and the
// atq.completed_at-based run-time / failure series — slices its day boundary
// in the viewer's tz, so all the dashboard tabs always agree.
export function dashboardUsageDailyOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
) {
  const queryKey = dashboardKeys.daily(wsId, days, projectId, tz);
  return queryOptions({
    queryKey,
    queryFn: () =>
      api.getDashboardUsageDaily({
        days,
        project_id: projectId ?? undefined,
        tz,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    refetchInterval: REFETCH_INTERVAL,
    placeholderData: (previousData, previousQuery) =>
      isSameDashboardScope(previousQuery?.queryKey, queryKey)
        ? keepPreviousData(previousData)
        : undefined,
  });
}

// Cost per deliverable (K04).
export function dashboardCostPerDeliverableOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
) {
  return queryOptions({
    queryKey: [...dashboardKeys.all(wsId), "cost-per-deliverable", days, projectId, tz] as const,
    queryFn: () =>
      api.getDashboardCostPerDeliverable({
        days,
        project_id: projectId ?? undefined,
        tz,
      }),
    enabled: !!wsId,
    staleTime: 60_000,
  });
}

// ROI per agent (JEF-252).
export function dashboardAgentRoiOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
) {
  return queryOptions({
    queryKey: [...dashboardKeys.all(wsId), "roi-by-agent", days, projectId, tz] as const,
    queryFn: () =>
      api.getDashboardAgentRoi({
        days,
        project_id: projectId ?? undefined,
        tz,
      }),
    enabled: !!wsId,
    staleTime: 60_000,
  });
}

// Mixed member/agent velocity (JEF-251). The server buckets weeks in UTC, so
// unlike the day-sliced rollups above there is no `tz` in the key.
export function dashboardVelocityWeeklyOptions(
  wsId: string,
  days: number,
  projectId: string | null,
) {
  return queryOptions({
    queryKey: dashboardKeys.velocityWeekly(wsId, days, projectId),
    queryFn: () =>
      api.getDashboardVelocityWeekly({
        days,
        projectId: projectId ?? undefined,
      }),
    enabled: !!wsId,
    staleTime: 60_000,
  });
}

const MS_PER_DAY = 24 * 60 * 60 * 1000;
const MS_PER_WEEK = 7 * MS_PER_DAY;

function mondayUtcMs(date: Date): number {
  const dayMs = Date.UTC(
    date.getUTCFullYear(),
    date.getUTCMonth(),
    date.getUTCDate(),
  );
  // getUTCDay: 0 = Sunday … 6 = Saturday; shift so Monday is offset 0.
  return dayMs - ((new Date(dayMs).getUTCDay() + 6) % 7) * MS_PER_DAY;
}

function isoDate(ms: number): string {
  return new Date(ms).toISOString().slice(0, 10);
}

/**
 * Fills the holes in the server-bucketed throughput series so a chart renders
 * one continuous week axis: every Monday from the oldest returned week to the
 * current week, with zero-count rows where the server had nothing. Duplicate
 * `week_start` rows are summed. When the server returned no weeks at all, the
 * `days` window still yields an all-zero axis so an empty chart has an x-axis.
 * Output is ascending by `week_start`.
 */
export function mergeThroughputWeeks(
  weeks: DashboardThroughputWeek[],
  days: number,
  now: Date = new Date(),
): DashboardThroughputWeek[] {
  const counts = new Map<string, { member_count: number; agent_count: number }>();
  let oldestMs: number | null = null;
  for (const week of weeks) {
    const ms = Date.parse(`${week.week_start}T00:00:00Z`);
    if (Number.isNaN(ms)) continue;
    const startMs = mondayUtcMs(new Date(ms));
    if (oldestMs === null || startMs < oldestMs) oldestMs = startMs;
    const existing = counts.get(isoDate(startMs));
    if (existing) {
      existing.member_count += week.member_count;
      existing.agent_count += week.agent_count;
    } else {
      counts.set(isoDate(startMs), {
        member_count: week.member_count,
        agent_count: week.agent_count,
      });
    }
  }

  const endMs = mondayUtcMs(now);
  let startMs: number;
  if (oldestMs !== null) {
    startMs = Math.min(oldestMs, endMs);
  } else {
    const windowWeeks = Math.max(1, Math.ceil(days / 7));
    startMs = endMs - (windowWeeks - 1) * MS_PER_WEEK;
  }

  const merged: DashboardThroughputWeek[] = [];
  for (let ms = startMs; ms <= endMs; ms += MS_PER_WEEK) {
    const weekStart = isoDate(ms);
    const row = counts.get(weekStart);
    merged.push({
      week_start: weekStart,
      member_count: row?.member_count ?? 0,
      agent_count: row?.agent_count ?? 0,
    });
  }
  return merged;
}

/**
 * Percentage change of a median cycle time against the previous period,
 * positive when delivery got slower. null when either side is missing or the
 * previous period was zero — same null rules as `roiTrendPct`.
 */
export function formatCycleTimeTrend(
  current: number | null,
  previous: number | null,
): number | null {
  return roiTrendPct(current, previous);
}

/**
 * Percentage change of a cost ratio against the previous period, negative when
 * the agent got cheaper. null when either side is missing or the previous
 * period was zero — there is no percentage change from nothing.
 */
export function roiTrendPct(current: number | null, previous: number | null): number | null {
  if (current === null || previous === null || previous === 0) return null;
  return ((current - previous) / previous) * 100;
}

export function dashboardUsageByAgentOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
) {
  const queryKey = dashboardKeys.byAgent(wsId, days, projectId, tz);
  return queryOptions({
    queryKey,
    queryFn: () =>
      api.getDashboardUsageByAgent({
        days,
        project_id: projectId ?? undefined,
        tz,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    refetchInterval: REFETCH_INTERVAL,
    placeholderData: (previousData, previousQuery) =>
      isSameDashboardScope(previousQuery?.queryKey, queryKey)
        ? keepPreviousData(previousData)
        : undefined,
  });
}

export function dashboardAgentRunTimeOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
) {
  const queryKey = dashboardKeys.agentRuntime(wsId, days, projectId, tz);
  return queryOptions({
    queryKey,
    queryFn: () =>
      api.getDashboardAgentRunTime({
        days,
        project_id: projectId ?? undefined,
        tz,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    refetchInterval: REFETCH_INTERVAL,
    placeholderData: (previousData, previousQuery) =>
      isSameDashboardScope(previousQuery?.queryKey, queryKey)
        ? keepPreviousData(previousData)
        : undefined,
  });
}

export function dashboardRunTimeDailyOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
) {
  const queryKey = dashboardKeys.runTimeDaily(wsId, days, projectId, tz);
  return queryOptions({
    queryKey,
    queryFn: () =>
      api.getDashboardRunTimeDaily({
        days,
        project_id: projectId ?? undefined,
        tz,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    refetchInterval: REFETCH_INTERVAL,
    placeholderData: (previousData, previousQuery) =>
      isSameDashboardScope(previousQuery?.queryKey, queryKey)
        ? keepPreviousData(previousData)
        : undefined,
  });
}

export function dashboardFailuresDailyOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
) {
  const queryKey = dashboardKeys.failuresDaily(wsId, days, projectId, tz);
  return queryOptions({
    queryKey,
    queryFn: () =>
      api.getDashboardFailuresDaily({
        days,
        project_id: projectId ?? undefined,
        tz,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    refetchInterval: REFETCH_INTERVAL,
    placeholderData: (previousData, previousQuery) =>
      isSameDashboardScope(previousQuery?.queryKey, queryKey)
        ? keepPreviousData(previousData)
        : undefined,
  });
}

export function dashboardFailuresByAgentOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
) {
  const queryKey = dashboardKeys.failuresByAgent(wsId, days, projectId, tz);
  return queryOptions({
    queryKey,
    queryFn: () =>
      api.getDashboardFailuresByAgent({
        days,
        project_id: projectId ?? undefined,
        tz,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    refetchInterval: REFETCH_INTERVAL,
    placeholderData: (previousData, previousQuery) =>
      isSameDashboardScope(previousQuery?.queryKey, queryKey)
        ? keepPreviousData(previousData)
        : undefined,
  });
}

/**
 * Smart-router benchmarks (JEF-237): the 90-day per-(runtime, provider,
 * model, task class) rollup. The window is fixed server-side, so the key
 * carries no days/project/tz — unlike the rollups above.
 */
export function routingStatsOptions(wsId: string) {
  return queryOptions({
    queryKey: dashboardKeys.routingStats(wsId),
    queryFn: () => api.listRoutingStats(),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    refetchInterval: REFETCH_INTERVAL,
  });
}

/**
 * Workflow-selector outcomes (JEF-273): the 90-day per-(task class, workflow)
 * rollup the selector learns from. The window is fixed server-side, so the
 * key carries no days/project/tz — same convention as `routingStatsOptions`.
 */
export function workflowStatsOptions(wsId: string) {
  return queryOptions({
    queryKey: dashboardKeys.workflowStats(wsId),
    queryFn: () => api.listWorkflowStats(),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    refetchInterval: REFETCH_INTERVAL,
  });
}
