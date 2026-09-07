"use client";

import { useMemo, useState } from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";
import {
  buildCalendarGrid,
  calendarMonthIsEmpty,
  currentCalendarMonth,
  shiftMonth,
  type CalendarCell,
  type CalendarMonth,
} from "@multica/core/issues/calendar-grid";
import { issueStatusCategory } from "@multica/core/issues";
import type { Issue, IssueStatusCategory } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { AppLink } from "../../navigation";
import { useWorkspacePaths } from "@multica/core/paths";
import { StatusIcon } from "./status-icon";
import { IssueActionsContextMenu } from "../actions";
import { useLocale, useT } from "../../i18n";

/**
 * Month calendar of issues by DUE DATE (F30 / JEF-34).
 *
 * Everything is UTC-day-aligned — the grid, the cell keys, the labels — for the
 * reason spelled out in `@multica/core/issues/calendar-grid`: `due_date` is a
 * calendar day, so anchoring it to local midnight would file an issue due Mar 1
 * under Feb 28 for every viewer west of UTC. The layout itself lives in that
 * pure module, which is where its matrix is tested; this file is the renderer.
 *
 * Due date, not start date: a calendar answers "what is landing when", and an
 * issue occupying every cell between its start and its due date is a Gantt bar
 * drawn badly. The Gantt already shows spans.
 */

// A cell shows this many chips before folding the rest behind "+N". Four keeps
// a six-row grid inside a laptop viewport without a scrollbar per cell.
const MAX_CHIPS_PER_CELL = 4;

const STATUS_DOT: Record<IssueStatusCategory, string> = {
  backlog: "bg-muted-foreground/60",
  todo: "bg-muted-foreground/70",
  in_progress: "bg-warning",
  in_review: "bg-success",
  done: "bg-info",
  blocked: "bg-destructive",
  cancelled: "bg-muted-foreground/40",
};

function IssueChip({ issue }: { issue: Issue }) {
  const p = useWorkspacePaths();
  return (
    <IssueActionsContextMenu issue={issue}>
      <AppLink
        href={p.issueDetail(issue.id)}
        newTabTitle={issue.identifier}
        className="flex min-w-0 items-center gap-1 rounded px-1 py-0.5 text-caption hover:bg-accent"
      >
        <span
          className={cn(
            "size-1.5 shrink-0 rounded-full",
            STATUS_DOT[issueStatusCategory(issue) ?? "todo"],
          )}
        />
        <span className="truncate">{issue.title}</span>
      </AppLink>
    </IssueActionsContextMenu>
  );
}

function DayCell({
  cell,
  overflowLabel,
  dayIssuesLabel,
}: {
  cell: CalendarCell<Issue>;
  overflowLabel: (count: number) => string;
  dayIssuesLabel: string;
}) {
  const p = useWorkspacePaths();
  const [open, setOpen] = useState(false);
  const visible = cell.items.slice(0, MAX_CHIPS_PER_CELL);
  const hidden = cell.items.length - visible.length;

  return (
    <div
      data-testid="calendar-cell"
      data-date={cell.date}
      className={cn(
        "flex min-h-24 flex-col gap-0.5 border-b border-r border-surface-border p-1",
        // Weekend shading is on the CELL, not the number, so a weekend reads
        // at a glance from across the grid.
        cell.isWeekend && "bg-muted/30",
        // Out-of-month cells stay legible rather than hidden: an issue due on
        // one is real, and blanking the cell would hide it.
        !cell.inMonth && "text-muted-foreground",
      )}
    >
      <span
        className={cn(
          "self-start rounded px-1 text-caption tabular-nums",
          cell.isToday
            ? "bg-brand font-medium text-brand-foreground"
            : cell.inMonth
              ? "text-foreground"
              : "text-faint-foreground",
        )}
      >
        {cell.utcDate.getUTCDate()}
      </span>
      {visible.map((issue) => (
        <IssueChip key={issue.id} issue={issue} />
      ))}
      {hidden > 0 && (
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger
            className="self-start rounded px-1 text-caption text-muted-foreground hover:bg-accent hover:text-foreground"
          >
            {overflowLabel(hidden)}
          </PopoverTrigger>
          {/* The popover carries the FULL day, not just the hidden tail: once
              a cell overflows, the useful question is "what is due that day",
              and answering it with the remainder would make the reader
              mentally re-join two lists. */}
          <PopoverContent align="start" className="w-64 p-1">
            <p className="px-1 pb-1 text-caption text-muted-foreground">
              {dayIssuesLabel}
            </p>
            <div className="flex max-h-72 flex-col gap-0.5 overflow-y-auto">
              {cell.items.map((issue) => (
                <IssueActionsContextMenu key={issue.id} issue={issue}>
                  <AppLink
                    href={p.issueDetail(issue.id)}
                    newTabTitle={issue.identifier}
                    className="flex min-w-0 items-center gap-1.5 rounded px-1 py-1 text-caption hover:bg-accent"
                  >
                    <StatusIcon
                      status={issue.status}
                      category={issueStatusCategory(issue) ?? undefined}
                      className="size-3.5 shrink-0"
                    />
                    <span className="w-14 shrink-0 tabular-nums text-muted-foreground">
                      {issue.identifier}
                    </span>
                    <span className="truncate">{issue.title}</span>
                  </AppLink>
                </IssueActionsContextMenu>
              ))}
            </div>
          </PopoverContent>
        </Popover>
      )}
    </div>
  );
}

export function CalendarView({ issues }: { issues: Issue[] }) {
  const { t } = useT("issues");
  const locale = useLocale();
  // Opened on the CURRENT UTC month, and held in local state rather than the
  // view store: which month you are looking at is a scroll position, not a
  // saved-view property, and persisting it would reopen a saved view on a
  // month that has since gone stale.
  const [month, setMonth] = useState<CalendarMonth>(() => currentCalendarMonth());
  const today = useMemo(() => new Date(), []);

  const cells = useMemo(
    () => buildCalendarGrid(month, issues, (issue) => issue.due_date, { today }),
    [issues, month, today],
  );
  const isEmpty = calendarMonthIsEmpty(cells);

  const monthLabel = new Date(Date.UTC(month.year, month.month - 1, 1)).toLocaleDateString(
    locale,
    { month: "long", year: "numeric", timeZone: "UTC" },
  );
  // Weekday headers are derived from the grid's own first row, so a future
  // Monday-first option cannot desynchronize the labels from the columns.
  const weekdayLabels = cells.slice(0, 7).map((cell) =>
    cell.utcDate.toLocaleDateString(locale, { weekday: "short", timeZone: "UTC" }),
  );

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <div className="flex h-9 shrink-0 items-center gap-2 border-b px-3">
        <Button
          size="icon-sm"
          variant="ghost"
          aria-label={t(($) => $.calendar.previous_month)}
          onClick={() => setMonth((m) => shiftMonth(m, -1))}
        >
          <ChevronLeft className="size-4" />
        </Button>
        <span className="min-w-40 text-body font-medium">{monthLabel}</span>
        <Button
          size="icon-sm"
          variant="ghost"
          aria-label={t(($) => $.calendar.next_month)}
          onClick={() => setMonth((m) => shiftMonth(m, 1))}
        >
          <ChevronRight className="size-4" />
        </Button>
        <Button
          size="sm"
          variant="outline"
          className="h-7 text-caption"
          onClick={() => setMonth(currentCalendarMonth())}
        >
          {t(($) => $.calendar.today)}
        </Button>
        <div className="flex-1" />
        <span className="text-caption text-muted-foreground">
          {t(($) => $.calendar.due_date_hint)}
        </span>
      </div>

      <div className="flex flex-1 min-h-0 flex-col overflow-auto">
        <div className="grid shrink-0 grid-cols-7 border-b bg-muted/20">
          {weekdayLabels.map((label, i) => (
            <div key={i} className="px-2 py-1 text-caption font-medium text-muted-foreground">
              {label}
            </div>
          ))}
        </div>
        {/* The empty state sits ABOVE the grid rather than replacing it: the
            month itself is the answer to "why is this empty", and hiding the
            calendar to say "nothing here" takes away the control that moves to
            a month that does have something. */}
        {isEmpty && (
          <p
            role="status"
            className="border-b px-3 py-2 text-caption text-muted-foreground"
          >
            {t(($) => $.calendar.empty_month)}
          </p>
        )}
        <div className="grid flex-1 grid-cols-7 border-l">
          {cells.map((cell) => (
            <DayCell
              key={cell.date}
              cell={cell}
              overflowLabel={(count) => t(($) => $.calendar.more, { count })}
              dayIssuesLabel={cell.utcDate.toLocaleDateString(locale, {
                month: "long",
                day: "numeric",
                timeZone: "UTC",
              })}
            />
          ))}
        </div>
      </div>
    </div>
  );
}
