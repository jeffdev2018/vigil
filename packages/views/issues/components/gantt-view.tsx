"use client";

import { useEffect, useMemo, useRef } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useViewStore, useViewStoreApi } from "@multica/core/issues/stores/view-store-context";
import type { GanttZoom } from "@multica/core/issues/stores/view-store";
import { projectListOptions } from "@multica/core/projects/queries";
import type { Issue, IssueStatusCategory } from "@multica/core/types";
import { issueStatusCategory } from "@multica/core/issues";
import { dateOnlyToUTCDate } from "@multica/core/issues/date";
import { issueDependencyEdgesOptions } from "@multica/core/issues/dependency-edges";
import {
  buildGanttArrows,
  ganttBarGeometry,
  type GanttBarGeometry,
} from "@multica/core/issues/gantt-arrows";
import { cn } from "@multica/ui/lib/utils";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { Button } from "@multica/ui/components/ui/button";
import { AppLink } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { ProjectIcon } from "../../projects/components/project-icon";
import { StatusIcon } from "./status-icon";
import { PriorityIcon } from "./priority-icon";
import { IssueActionsContextMenu } from "../actions";
import { sortIssues } from "../utils/sort";
import { useLocale, useT } from "../../i18n";

// ---------------------------------------------------------------------------
// Date utilities — everything is UTC-day-aligned so a `due_date` ISO string
// produced anywhere maps to exactly one column on the axis.
// ---------------------------------------------------------------------------

const MS_PER_DAY = 24 * 60 * 60 * 1000;

function startOfDayUTC(d: Date): Date {
  return new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate()));
}

function addDays(d: Date, days: number): Date {
  return new Date(d.getTime() + days * MS_PER_DAY);
}

function daysBetween(a: Date, b: Date): number {
  return Math.round((b.getTime() - a.getTime()) / MS_PER_DAY);
}

// Issue dates arrive as date-only "YYYY-MM-DD" strings (calendar days). Anchor
// each to UTC midnight so the bar lands on exactly that day, independent of the
// viewer's timezone. See @multica/core/issues/date.
function parseDay(iso: string | null): Date | null {
  return dateOnlyToUTCDate(iso);
}

function isWeekendUTC(d: Date): boolean {
  const wd = d.getUTCDay();
  return wd === 0 || wd === 6;
}

function isMonthStartUTC(d: Date): boolean {
  return d.getUTCDate() === 1;
}

function isWeekStartUTC(d: Date): boolean {
  return d.getUTCDay() === 1; // Monday
}

// ---------------------------------------------------------------------------
// Geometry
// ---------------------------------------------------------------------------

const ROW_HEIGHT = 36;
/**
 * Rows past this count are windowed (F30). Below it every row is mounted, which
 * is what the canvas always did: a 40-row chart gains nothing from a window and
 * pays for a measured scroll container. `top` is `index * ROW_HEIGHT` in BOTH
 * modes, so the arrow layer's geometry is identical either way.
 */
const GANTT_VIRTUALIZE_THRESHOLD = 60;
const HEADER_HEIGHT = 56;
const LEFT_COL_WIDTH = 320;

const DAY_PX_BY_ZOOM: Record<GanttZoom, number> = {
  day: 36,
  week: 14,
  month: 6,
};

interface Range {
  start: Date;
  end: Date;
}

function computeRange(issues: Issue[], today: Date, zoom: GanttZoom): Range {
  const defaultPad: Record<GanttZoom, number> = {
    day: 21,
    week: 60,
    month: 180,
  };
  let minTs = today.getTime() - defaultPad[zoom] * MS_PER_DAY;
  let maxTs = today.getTime() + defaultPad[zoom] * MS_PER_DAY;
  for (const i of issues) {
    const s = parseDay(i.start_date);
    const e = parseDay(i.due_date);
    if (s && s.getTime() < minTs) minTs = s.getTime();
    if (e && e.getTime() > maxTs) maxTs = e.getTime();
    if (s && s.getTime() > maxTs) maxTs = s.getTime();
    if (e && e.getTime() < minTs) minTs = e.getTime();
  }
  const pad = Math.max(2, Math.round(defaultPad[zoom] / 6));
  return {
    start: addDays(startOfDayUTC(new Date(minTs)), -pad),
    end: addDays(startOfDayUTC(new Date(maxTs)), pad + 1),
  };
}

// ---------------------------------------------------------------------------
// Top axis — sticky on vertical scroll. Renders month + day/week ticks.
// ---------------------------------------------------------------------------

function GanttAxis({
  range,
  dayPx,
  zoom,
  todayOffsetDays,
  width,
}: {
  range: Range;
  dayPx: number;
  zoom: GanttZoom;
  todayOffsetDays: number;
  width: number;
}) {
  const locale = useLocale();
  const totalDays = daysBetween(range.start, range.end);

  const monthBlocks = useMemo(() => {
    const out: { label: string; left: number; width: number }[] = [];
    let cursor = startOfDayUTC(range.start);
    while (cursor.getTime() < range.end.getTime()) {
      const monthEnd = new Date(
        Date.UTC(cursor.getUTCFullYear(), cursor.getUTCMonth() + 1, 1),
      );
      const blockEnd = monthEnd.getTime() > range.end.getTime() ? range.end : monthEnd;
      const startDays = daysBetween(range.start, cursor);
      const widthDays = daysBetween(cursor, blockEnd);
      out.push({
        label: cursor.toLocaleDateString(locale, {
          month: "short",
          year: "numeric",
          timeZone: "UTC",
        }),
        left: startDays * dayPx,
        width: widthDays * dayPx,
      });
      cursor = monthEnd;
    }
    return out;
  }, [range, dayPx, locale]);

  return (
    <div
      className="relative shrink-0 border-b bg-background"
      style={{ height: HEADER_HEIGHT, width }}
    >
      {/* Month row */}
      <div className="relative h-7 border-b">
        {monthBlocks.map((b, i) => (
          <div
            key={i}
            className="absolute top-0 bottom-0 flex items-center px-2 text-caption font-medium text-foreground"
            style={{ left: b.left, width: b.width }}
          >
            {b.width > 40 && <span className="truncate">{b.label}</span>}
          </div>
        ))}
      </div>
      {/* Day / week ticks */}
      <div className="relative h-7">
        {Array.from({ length: totalDays }, (_, i) => {
          const date = addDays(range.start, i);
          const isMonth = isMonthStartUTC(date);
          const isWeek = isWeekStartUTC(date);
          const showLabel =
            zoom === "day" ||
            (zoom === "week" && isWeek) ||
            (zoom === "month" && isMonth);
          return (
            <div
              key={i}
              className={cn(
                "absolute top-0 bottom-0 flex items-center justify-center text-micro text-muted-foreground border-l",
                isMonth
                  ? "border-foreground/15"
                  : isWeek
                  ? "border-foreground/10"
                  : "border-foreground/5",
              )}
              style={{ left: i * dayPx, width: dayPx }}
            >
              {showLabel && (
                <div className="flex flex-col items-center leading-tight">
                  {zoom === "day" && (
                    <>
                      <span className="tabular-nums">{date.getUTCDate()}</span>
                      <span className="text-micro">
                        {date.toLocaleDateString(locale, {
                          weekday: "short",
                          timeZone: "UTC",
                        })}
                      </span>
                    </>
                  )}
                  {zoom === "week" && (
                    <span className="tabular-nums">{date.getUTCDate()}</span>
                  )}
                  {zoom === "month" && (
                    <span className="tabular-nums whitespace-nowrap">
                      {date.toLocaleDateString(locale, {
                        month: "short",
                        day: "numeric",
                        timeZone: "UTC",
                      })}
                    </span>
                  )}
                </div>
              )}
            </div>
          );
        })}
        {todayOffsetDays >= 0 && todayOffsetDays <= totalDays && (
          <div
            className="absolute top-0 bottom-0 w-px bg-brand"
            style={{ left: todayOffsetDays * dayPx }}
          />
        )}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Background layer — weekend shading, week/month gridlines, today line.
// Rendered once across the full timeline track height behind all bars.
// ---------------------------------------------------------------------------

function BackgroundLayer({
  range,
  dayPx,
  height,
  todayOffsetDays,
}: {
  range: Range;
  dayPx: number;
  height: number;
  todayOffsetDays: number;
}) {
  const totalDays = daysBetween(range.start, range.end);
  return (
    <div
      className="pointer-events-none absolute inset-0"
      style={{ height, width: totalDays * dayPx }}
    >
      {Array.from({ length: totalDays }, (_, i) => {
        const date = addDays(range.start, i);
        const weekend = isWeekendUTC(date);
        const isMonth = isMonthStartUTC(date);
        const isWeek = isWeekStartUTC(date);
        return (
          <div
            key={i}
            className="absolute top-0 bottom-0"
            style={{ left: i * dayPx, width: dayPx }}
          >
            {weekend && <div className="absolute inset-0 bg-muted/40" />}
            {(isMonth || isWeek) && (
              <div
                className={cn(
                  "absolute top-0 bottom-0 left-0 w-px",
                  isMonth ? "bg-foreground/10" : "bg-foreground/5",
                )}
              />
            )}
          </div>
        );
      })}
      {todayOffsetDays >= 0 && todayOffsetDays <= totalDays && (
        <div
          className="absolute top-0 bottom-0 w-px bg-brand/70"
          style={{ left: todayOffsetDays * dayPx }}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Bar color by status (uses semantic Tailwind tokens, not hardcoded colors).
// ---------------------------------------------------------------------------

// Keyed by CATEGORY, not by status key: an issue on a custom status draws in
// the color of the category it behaves as. Keying this by IssueStatus made the
// lookup `undefined` for every custom key, so the bar lost its color entirely.
// (MUL-6243)
const STATUS_BAR_BG: Record<IssueStatusCategory, string> = {
  backlog: "bg-muted-foreground/60",
  todo: "bg-muted-foreground/70",
  in_progress: "bg-warning",
  in_review: "bg-success",
  done: "bg-info",
  blocked: "bg-destructive",
  cancelled: "bg-muted-foreground/40",
};

// ---------------------------------------------------------------------------
// One row — left label cell + right timeline track with absolute bar.
// ---------------------------------------------------------------------------

function ScheduledRow({
  issue,
  range,
  dayPx,
  totalDays,
}: {
  issue: Issue;
  range: Range;
  dayPx: number;
  totalDays: number;
}) {
  const { t } = useT("issues");
  const locale = useLocale();
  const p = useWorkspacePaths();
  const wsId = useWorkspaceId();
  const { data: projects = [] } = useQuery({
    ...projectListOptions(wsId),
    enabled: !!issue.project_id,
  });
  const project = issue.project_id ? projects.find((pr) => pr.id === issue.project_id) : undefined;

  const start = parseDay(issue.start_date);
  const due = parseDay(issue.due_date);

  // start > due is a data anomaly (backend only validates RFC3339, not order).
  // Normalize to min/max so the row still draws something, and flag it so the
  // user notices instead of seeing a silently empty row.
  const inverted =
    start !== null && due !== null && start.getTime() > due.getTime();
  const rangeStart = start && due ? (inverted ? due : start) : (start ?? due);
  const rangeEnd = start && due ? (inverted ? start : due) : (start ?? due);

  let bar: { left: number; width: number; isMarker: boolean } | null = null;
  if (rangeStart && rangeEnd) {
    const s = Math.max(daysBetween(range.start, rangeStart), 0);
    const e = Math.min(daysBetween(range.start, rangeEnd) + 1, totalDays);
    if (e > s) {
      const isSingle = !start || !due;
      if (isSingle) {
        bar = { left: s * dayPx, width: Math.max(dayPx, 12), isMarker: true };
      } else {
        bar = { left: s * dayPx, width: (e - s) * dayPx, isMarker: false };
      }
    }
  }

  const fmt = (d: Date) =>
    d.toLocaleDateString(locale, {
      month: "short",
      day: "numeric",
      year: "numeric",
      timeZone: "UTC",
    });

  return (
    <IssueActionsContextMenu issue={issue}>
      <div
        className="flex border-b border-foreground/5 hover:bg-accent/30 transition-colors"
        style={{ height: ROW_HEIGHT }}
      >
        {/* Sticky label cell */}
        <AppLink
          href={p.issueDetail(issue.id)}
          newTabTitle={issue.identifier}
          className="sticky left-0 z-[1] flex shrink-0 items-center gap-2 border-r bg-background px-3 text-body min-w-0"
          style={{ width: LEFT_COL_WIDTH }}
        >
          <StatusIcon
            status={issue.status}
            category={issueStatusCategory(issue) ?? undefined}
            className="h-3.5 w-3.5"
          />
          <PriorityIcon priority={issue.priority} />
          <span className="w-14 shrink-0 text-caption text-muted-foreground tabular-nums truncate">
            {issue.identifier}
          </span>
          <span className="truncate flex-1">{issue.title}</span>
          {project && <ProjectIcon project={project} size="sm" />}
          {issue.assignee_type && issue.assignee_id && (
            <ActorAvatar
              actorType={issue.assignee_type}
              actorId={issue.assignee_id}
              size="sm"
              enableHoverCard
            />
          )}
        </AppLink>
        {/* Timeline track */}
        <div
          className="relative shrink-0"
          style={{ width: totalDays * dayPx }}
        >
          {bar && (
            <Tooltip>
              <TooltipTrigger
                render={
                  <AppLink
                    href={p.issueDetail(issue.id)}
                    newTabTitle={issue.identifier}
                    className={cn(
                      "absolute top-1/2 -translate-y-1/2 transition-opacity hover:opacity-90",
                      bar.isMarker
                        ? "h-3 w-3 rotate-45 rounded-[2px]"
                        : "h-5 rounded-md",
                      STATUS_BAR_BG[issueStatusCategory(issue) ?? "todo"],
                      inverted && "ring-2 ring-destructive ring-offset-1 ring-offset-background",
                    )}
                    style={{ left: bar.left, width: bar.width }}
                  >
                    {!bar.isMarker && bar.width > 60 && (
                      <span className="block truncate px-2 py-[2px] text-micro leading-4 text-white">
                        {issue.title}
                      </span>
                    )}
                  </AppLink>
                }
              />
              <TooltipContent side="top">
                <div className="flex flex-col gap-0.5 text-caption">
                  <span className="font-medium">{issue.title}</span>
                  <span className="text-muted-foreground">
                    {start ? fmt(start) : "—"} → {due ? fmt(due) : "—"}
                  </span>
                  {inverted && (
                    <span className="text-destructive">
                      {t(($) => $.gantt.inverted_dates_warning)}
                    </span>
                  )}
                </div>
              </TooltipContent>
            </Tooltip>
          )}
        </div>
      </div>
    </IssueActionsContextMenu>
  );
}

// ---------------------------------------------------------------------------
// Dependency arrows — one SVG over the whole track (F30)
// ---------------------------------------------------------------------------

/**
 * Draws one arrow per `blocks` dependency between two DATED issues on the
 * canvas.
 *
 * Rendered as a single absolutely-positioned SVG spanning the full track rather
 * than per row, for the reason the geometry module spells out: every coordinate
 * comes from a row INDEX and a date, so an arrow between two rows that
 * virtualization has not mounted still lands in the right place. A per-row
 * overlay would have to measure the DOM and would lose exactly those arrows.
 */
function DependencyArrowLayer({
  issues,
  range,
  dayPx,
  totalDays,
  height,
}: {
  issues: Issue[];
  range: Range;
  dayPx: number;
  totalDays: number;
  height: number;
}) {
  const wsId = useWorkspaceId();
  const issueIds = useMemo(() => issues.map((issue) => issue.id), [issues]);
  const { data: edges = [] } = useQuery({
    ...issueDependencyEdgesOptions(wsId, issueIds),
    enabled: issueIds.length > 1,
  });

  const arrows = useMemo(() => {
    const geometry = new Map<string, GanttBarGeometry>();
    issues.forEach((issue, index) => {
      const bar = ganttBarGeometry(issue, index, range.start, totalDays);
      if (bar) geometry.set(issue.id, bar);
    });
    return buildGanttArrows(edges, geometry, { dayPx, rowHeight: ROW_HEIGHT });
  }, [dayPx, edges, issues, range.start, totalDays]);

  if (arrows.length === 0) return null;

  return (
    <svg
      data-testid="gantt-dependency-arrows"
      className="pointer-events-none absolute left-0 top-0 overflow-visible"
      width={totalDays * dayPx}
      height={height}
      aria-hidden
    >
      {arrows.map((arrow) => (
        <g key={arrow.id}>
          <path
            d={arrow.path}
            fill="none"
            stroke="currentColor"
            strokeWidth={1.5}
            className="text-faint-foreground"
          />
          {/* The head is drawn inline rather than through an SVG <marker>:
              markers inherit neither `currentColor` nor the theme token in
              every engine, and a two-point polygon costs less than debugging
              that. */}
          <polygon
            points={`${arrow.headX},${arrow.headY} ${arrow.headX - 5},${arrow.headY - 3.5} ${arrow.headX - 5},${arrow.headY + 3.5}`}
            className="fill-current text-faint-foreground"
          />
        </g>
      ))}
    </svg>
  );
}

// ---------------------------------------------------------------------------
// GanttView — public component
// ---------------------------------------------------------------------------

export function GanttView({ issues }: { issues: Issue[] }) {
  const { t } = useT("issues");
  const zoom = useViewStore((s) => s.ganttZoom);
  const showCompleted = useViewStore((s) => s.ganttShowCompleted);
  const sortBy = useViewStore((s) => s.sortBy);
  const sortDirection = useViewStore((s) => s.sortDirection);
  const act = useViewStoreApi().getState();

  const today = useMemo(() => startOfDayUTC(new Date()), []);
  const dayPx = DAY_PX_BY_ZOOM[zoom];

  // `issues` is already the canvas set: the surface applies the shared
  // filters, drops undated rows, and honours `ganttShowCompleted` before
  // handing it over (see `ganttCanvasRows` in use-issue-surface-data.ts).
  // Those rules used to live here, which meant the header chip could count
  // rows this canvas would never draw (MUL-4884). Keep this view a renderer:
  // it orders rows, it does not decide which ones exist.
  const scheduled = useMemo(() => {
    // "position" makes no sense on a gantt — default to start_date asc when
    // the user hasn't picked a more specific sort.
    const sortField = sortBy === "position" ? "start_date" : sortBy;
    return sortIssues(issues, sortField, sortDirection);
  }, [issues, sortBy, sortDirection]);

  const range = useMemo(
    () => computeRange(scheduled, today, zoom),
    [scheduled, today, zoom],
  );
  const totalDays = daysBetween(range.start, range.end);
  const timelineWidth = totalDays * dayPx;
  const todayOffsetDays = daysBetween(range.start, today);

  const scrollRef = useRef<HTMLDivElement | null>(null);
  // Virtualization kicks in only past a threshold. Below it the canvas mounts
  // every row, which is what it always did and costs nothing at this size —
  // and it keeps the common Gantt out of a window that depends on a measured
  // scroll container (a container with no layout, as in a test environment,
  // reports zero height and would render an empty chart).
  const virtualized = scheduled.length > GANTT_VIRTUALIZE_THRESHOLD;
  const rowVirtualizer = useVirtualizer({
    count: virtualized ? scheduled.length : 0,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_HEIGHT,
    // Rows are a fixed height, so the estimate is exact and no measurement pass
    // is needed. The overscan keeps a screenful of rows mounted either side, so
    // a fast scroll does not flash empty bands.
    overscan: 12,
  });
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const target = Math.max(0, LEFT_COL_WIDTH + todayOffsetDays * dayPx - 240);
    el.scrollLeft = target;
  }, [todayOffsetDays, dayPx]);

  if (scheduled.length === 0) {
    return (
      <div className="flex-1 min-h-0 flex items-center justify-center text-body text-muted-foreground">
        {t(($) => $.gantt.empty)}
      </div>
    );
  }

  return (
    <div className="flex flex-col flex-1 min-h-0">
      {/* Toolbar */}
      <div className="flex h-9 shrink-0 items-center gap-2 border-b px-3">
        <div className="inline-flex items-center rounded-md border border-foreground/10 p-0.5">
          {([
            { value: "day", label: t(($) => $.gantt.zoom_day) },
            { value: "week", label: t(($) => $.gantt.zoom_week) },
            { value: "month", label: t(($) => $.gantt.zoom_month) },
          ] as const).map((opt) => (
            <Button
              key={opt.value}
              size="sm"
              variant={zoom === opt.value ? "secondary" : "ghost"}
              className={cn(
                "h-6 px-2 text-caption",
                zoom !== opt.value && "text-muted-foreground",
              )}
              onClick={() => act.setGanttZoom(opt.value)}
            >
              {opt.label}
            </Button>
          ))}
        </div>
        <div className="flex-1" />
        <Button
          size="sm"
          variant={showCompleted ? "secondary" : "outline"}
          className={cn(
            "h-7 text-caption",
            !showCompleted && "text-muted-foreground",
          )}
          onClick={act.toggleGanttShowCompleted}
        >
          {t(($) => $.gantt.show_completed)}
        </Button>
      </div>

      {/* Body — single scroll container drives both vertical + horizontal */}
      <div ref={scrollRef} className="flex-1 min-h-0 overflow-auto">
        <div style={{ minWidth: LEFT_COL_WIDTH + timelineWidth }}>
          {/* Sticky header row */}
          <div className="sticky top-0 z-20 flex">
            <div
              className="sticky left-0 z-30 shrink-0 border-b border-r bg-background"
              style={{ width: LEFT_COL_WIDTH, height: HEADER_HEIGHT }}
            >
              <div className="flex h-full items-end px-3 pb-1.5 text-micro font-medium text-muted-foreground">
                {t(($) => $.gantt.header_issue)}
              </div>
            </div>
            <GanttAxis
              range={range}
              dayPx={dayPx}
              zoom={zoom}
              todayOffsetDays={todayOffsetDays}
              width={timelineWidth}
            />
          </div>

          {/* Scheduled rows + background overlay.
              VIRTUALIZED (F30): the canvas is a fully materialized window, and
              a project with several hundred dated issues used to mount one row
              — each with its own tooltip, context menu and avatar — per issue.
              Row height is a constant here, so the virtualizer needs no
              measurement and the absolute geometry the arrow layer derives
              from row indices stays exact. */}
          <div
            className="relative"
            style={{ height: scheduled.length * ROW_HEIGHT }}
          >
            {/* Background gridlines + today line spanning all rows. Positioned
                starting after the left label column. */}
            <div
              className="pointer-events-none absolute top-0"
              style={{ left: LEFT_COL_WIDTH, width: timelineWidth }}
            >
              <BackgroundLayer
                range={range}
                dayPx={dayPx}
                height={scheduled.length * ROW_HEIGHT}
                todayOffsetDays={todayOffsetDays}
              />
              <DependencyArrowLayer
                issues={scheduled}
                range={range}
                dayPx={dayPx}
                totalDays={totalDays}
                height={scheduled.length * ROW_HEIGHT}
              />
            </div>
            {(virtualized
              ? rowVirtualizer.getVirtualItems().map((virtualRow) => virtualRow.index)
              : scheduled.map((_, index) => index)
            ).map((index) => {
              const issue = scheduled[index];
              if (!issue) return null;
              return (
                <div
                  key={issue.id}
                  className="absolute left-0 right-0"
                  style={{ top: index * ROW_HEIGHT, height: ROW_HEIGHT }}
                >
                  <ScheduledRow
                    issue={issue}
                    range={range}
                    dayPx={dayPx}
                    totalDays={totalDays}
                  />
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}
