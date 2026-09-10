"use client";

import { useMemo, useState } from "react";
import { AlarmClock, CalendarRange, ChevronLeft, ChevronRight, Plus } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import {
  buildCalendarGrid,
  calendarMonthIsEmpty,
  currentCalendarMonth,
  shiftMonth,
  weekStartsOnFor,
  type CalendarMonth,
} from "@multica/core/issues/calendar-grid";
import { calendarAgendaOptions, groupAgendaByDay, type CalendarDayBucket } from "@multica/core/calendar-events";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import type { AgendaCycle, AgendaFollowup, AgendaIssue, AgendaMeeting, CalendarEventEntry } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { AppLink } from "../../navigation";
import { SegmentedToggle } from "../../common/segmented-toggle";
import { useViewingTimezone } from "../../common/use-viewing-timezone";
import { CollectionPageHeader, CollectionPageHeaderAction } from "../../layout/collection-page";
import { CalendarEventDialog, type CalendarEventDialogTarget } from "./calendar-event-dialog";
import { CalendarEventSheet } from "./calendar-event-sheet";
import { useLocale, useT } from "../../i18n";

/**
 * Native calendar (OS plan, chantier 19): the workspace's events joined with
 * issue due dates, cycles and recorded meetings. The month grid reuses
 * `@multica/core/issues/calendar-grid` and the day-cell look of
 * `CalendarView` (issues due by day); a week/agenda toggle covers the two
 * other ways to read the same window.
 */

const MS_PER_DAY = 24 * 60 * 60 * 1000;
const MAX_ENTRIES_PER_CELL = 4;

type ViewMode = "month" | "week" | "agenda";

type DayEntry =
  | { kind: "event"; key: string; event: CalendarEventEntry }
  | { kind: "issue"; key: string; issue: AgendaIssue }
  | { kind: "meeting"; key: string; meeting: AgendaMeeting }
  | { kind: "cycle"; key: string; cycle: AgendaCycle }
  | { kind: "followup"; key: string; followup: AgendaFollowup };

function addDaysUTC(d: Date, days: number): Date {
  return new Date(d.getTime() + days * MS_PER_DAY);
}

function startOfWeekUTC(d: Date, weekStartsOn: 0 | 1): Date {
  const start = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate()));
  return addDaysUTC(start, -((start.getUTCDay() - weekStartsOn + 7) % 7));
}

function utcDayKey(d: Date): string {
  return d.toISOString().slice(0, 10);
}

function entriesForDay(dateKey: string, bucket: CalendarDayBucket | undefined, cycles: AgendaCycle[]): DayEntry[] {
  const entries: DayEntry[] = [];
  for (const cycle of cycles) {
    if (cycle.start_date <= dateKey && dateKey <= cycle.end_date) {
      entries.push({ kind: "cycle", key: `cycle:${cycle.id}`, cycle });
    }
  }
  if (bucket) {
    for (const event of bucket.events) entries.push({ kind: "event", key: `event:${event.id}`, event });
    for (const issue of bucket.issuesDue) entries.push({ kind: "issue", key: `issue:${issue.id}`, issue });
    for (const meeting of bucket.meetings) entries.push({ kind: "meeting", key: `meeting:${meeting.id}`, meeting });
    for (const followup of bucket.followups) entries.push({ kind: "followup", key: `followup:${followup.id}`, followup });
  }
  return entries;
}

function EntryChip({ entry, onOpenEvent }: { entry: DayEntry; onOpenEvent: (id: string) => void }) {
  const p = useWorkspacePaths();
  const { t } = useT("calendar-events");
  if (entry.kind === "followup") {
    // A scheduled wake-up of an issue's agent. The note is the whole point of
    // the wake-up, so it rides in the title where a truncated row can still
    // be read, and the chip links to the issue it will wake up on.
    const f = entry.followup;
    return (
      <AppLink
        href={p.issueDetail(f.issue_id)}
        newTabTitle={f.identifier}
        data-testid="agenda-followup"
        title={f.note}
        className="flex min-w-0 items-center gap-1 rounded px-1 py-0.5 text-caption hover:bg-accent"
      >
        <AlarmClock className="size-3 shrink-0 text-warning" aria-hidden="true" />
        <span className="shrink-0 text-warning">{t(($) => $.page.followup_prefix)}</span>
        <span className="shrink-0 truncate text-muted-foreground">{f.agent_name}</span>
        <span className="w-12 shrink-0 truncate text-muted-foreground">{f.identifier}</span>
        <span className="truncate">{f.note}</span>
      </AppLink>
    );
  }
  if (entry.kind === "cycle") {
    return (
      <span className="truncate rounded bg-primary/10 px-1 py-0.5 text-caption text-primary">
        {entry.cycle.name}
      </span>
    );
  }
  if (entry.kind === "issue") {
    return (
      <AppLink
        href={p.issueDetail(entry.issue.id)}
        newTabTitle={entry.issue.identifier}
        className="flex min-w-0 items-center gap-1 rounded px-1 py-0.5 text-caption hover:bg-accent"
      >
        <span className="w-10 shrink-0 truncate text-muted-foreground">{entry.issue.identifier}</span>
        <span className="truncate">{entry.issue.title}</span>
      </AppLink>
    );
  }
  if (entry.kind === "meeting") {
    return (
      <span className="flex items-center gap-1 truncate rounded px-1 py-0.5 text-caption text-muted-foreground">
        <span className="size-1.5 shrink-0 rounded-full bg-muted-foreground/60" />
        <span className="truncate">{entry.meeting.title}</span>
      </span>
    );
  }
  const event = entry.event;
  return (
    <button
      type="button"
      onClick={() => onOpenEvent(event.id)}
      className={cn(
        "flex min-w-0 items-center gap-1 rounded border px-1 py-0.5 text-left text-caption hover:bg-accent",
        event.status === "proposed" ? "border-dashed border-warning/60" : "border-transparent",
        event.status === "cancelled" && "text-muted-foreground line-through",
      )}
    >
      <span
        className={cn(
          "size-1.5 shrink-0 rounded-full",
          event.status === "cancelled" ? "bg-muted-foreground/40" : "bg-brand",
        )}
      />
      <span className="truncate">{event.title}</span>
    </button>
  );
}

function DayCellView({
  dateKey,
  utcDate,
  isToday,
  isWeekend,
  inMonth,
  entries,
  onOpenEvent,
  overflowLabel,
  dayLabel,
}: {
  dateKey: string;
  utcDate: Date;
  isToday: boolean;
  isWeekend: boolean;
  inMonth: boolean;
  entries: DayEntry[];
  onOpenEvent: (id: string) => void;
  overflowLabel: (count: number) => string;
  dayLabel: string;
}) {
  const [open, setOpen] = useState(false);
  const visible = entries.slice(0, MAX_ENTRIES_PER_CELL);
  const hidden = entries.length - visible.length;

  return (
    <div
      data-testid="calendar-cell"
      data-date={dateKey}
      className={cn(
        "flex min-h-24 flex-col gap-0.5 border-b border-r border-surface-border p-1",
        isWeekend && "bg-muted/30",
        !inMonth && "text-muted-foreground",
      )}
    >
      <span
        className={cn(
          "self-start rounded px-1 text-caption tabular-nums",
          isToday
            ? "bg-brand font-medium text-brand-foreground"
            : inMonth
              ? "text-foreground"
              : "text-faint-foreground",
        )}
      >
        {utcDate.getUTCDate()}
      </span>
      {visible.map((entry) => (
        <EntryChip key={entry.key} entry={entry} onOpenEvent={onOpenEvent} />
      ))}
      {hidden > 0 && (
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger className="self-start rounded px-1 text-caption text-muted-foreground hover:bg-accent hover:text-foreground">
            {overflowLabel(hidden)}
          </PopoverTrigger>
          <PopoverContent align="start" className="w-64 p-1">
            <p className="px-1 pb-1 text-caption text-muted-foreground">{dayLabel}</p>
            <div className="flex max-h-72 flex-col gap-0.5 overflow-y-auto">
              {entries.map((entry) => (
                <EntryChip key={entry.key} entry={entry} onOpenEvent={onOpenEvent} />
              ))}
            </div>
          </PopoverContent>
        </Popover>
      )}
    </div>
  );
}

export function CalendarPage() {
  const { t } = useT("calendar-events");
  const locale = useLocale();
  const wsId = useWorkspaceId();
  const tz = useViewingTimezone();
  const today = useMemo(() => new Date(), []);

  const [view, setView] = useState<ViewMode>("month");
  const [month, setMonth] = useState<CalendarMonth>(() => currentCalendarMonth());
  const weekStartsOn = weekStartsOnFor(locale);
  const [weekStart, setWeekStart] = useState<Date>(() => startOfWeekUTC(new Date(), weekStartsOn));
  const [dialogTarget, setDialogTarget] = useState<CalendarEventDialogTarget | null>(null);
  const [openEventId, setOpenEventId] = useState<string | null>(null);

  // The month grid's own 42-cell shape gives us the window's exact bounds
  // (leading/trailing days from neighbouring months included) without
  // recomputing the "6 weeks, Sunday-first" arithmetic a second time here.
  const monthCells = useMemo(
    () => buildCalendarGrid(month, [] as AgendaIssue[], () => null, { today, weekStartsOn }),
    [month, today, weekStartsOn],
  );
  const isMonth = view === "month";
  const from = isMonth ? monthCells[0]!.utcDate : weekStart;
  const to = isMonth ? addDaysUTC(monthCells[monthCells.length - 1]!.utcDate, 1) : addDaysUTC(weekStart, 7);

  const { data: agenda } = useQuery(calendarAgendaOptions(wsId, from.toISOString(), to.toISOString()));

  // Month bucketing stays UTC (matching the issues calendar's due-date
  // convention, and the grid's own day keys); week/agenda bucket in the
  // viewer's timezone, where an event's local day is what a list view should
  // show it under.
  const days = useMemo(
    () => (agenda ? groupAgendaByDay(agenda, isMonth ? "UTC" : tz) : new Map<string, CalendarDayBucket>()),
    [agenda, isMonth, tz],
  );
  const cycles = agenda?.cycles ?? [];

  const monthLabel = new Date(Date.UTC(month.year, month.month - 1, 1)).toLocaleDateString(locale, {
    month: "long",
    year: "numeric",
    timeZone: "UTC",
  });
  const weekLabel = `${weekStart.toLocaleDateString(locale, { month: "short", day: "numeric", timeZone: "UTC" })} – ${addDaysUTC(weekStart, 6).toLocaleDateString(locale, { month: "short", day: "numeric", timeZone: "UTC" })}`;

  const weekDayKeys = useMemo(
    () => Array.from({ length: 7 }, (_, i) => utcDayKey(addDaysUTC(weekStart, i))),
    [weekStart],
  );

  const isEmpty =
    isMonth
      ? !agenda || (calendarMonthIsEmpty(monthCells) && agenda.events.length === 0 && agenda.cycles.length === 0)
      : !agenda ||
        (agenda.events.length === 0 &&
          agenda.issues_due.length === 0 &&
          agenda.meetings.length === 0 &&
          (agenda.followups?.length ?? 0) === 0);

  const editingEvent = openEventId
    ? agenda?.events.find((e) => e.id === openEventId)
    : undefined;

  return (
    <div className="relative flex flex-1 min-h-0 flex-col">
      <CollectionPageHeader
        icon={CalendarRange}
        title={t(($) => $.page.title)}
        actions={
          <div className="flex items-center gap-2">
            <SegmentedToggle<ViewMode>
              value={view}
              onChange={setView}
              options={[
                ["month", t(($) => $.page.view_month)],
                ["week", t(($) => $.page.view_week)],
                ["agenda", t(($) => $.page.view_agenda)],
              ]}
            />
            <CollectionPageHeaderAction
              icon={Plus}
              label={t(($) => $.page.new_event)}
              onClick={() => setDialogTarget({ mode: "create" })}
            />
          </div>
        }
      />

      <div className="flex h-9 shrink-0 items-center gap-2 border-b px-3">
        <Button
          size="icon-sm"
          variant="ghost"
          aria-label={isMonth ? t(($) => $.page.previous_month) : t(($) => $.page.previous_period)}
          onClick={() =>
            isMonth ? setMonth((m) => shiftMonth(m, -1)) : setWeekStart((d) => addDaysUTC(d, -7))
          }
        >
          <ChevronLeft className="size-4" />
        </Button>
        <span className="min-w-32 text-body font-medium">{isMonth ? monthLabel : weekLabel}</span>
        <Button
          size="icon-sm"
          variant="ghost"
          aria-label={isMonth ? t(($) => $.page.next_month) : t(($) => $.page.next_period)}
          onClick={() =>
            isMonth ? setMonth((m) => shiftMonth(m, 1)) : setWeekStart((d) => addDaysUTC(d, 7))
          }
        >
          <ChevronRight className="size-4" />
        </Button>
        <Button
          size="sm"
          variant="outline"
          className="h-7 text-caption"
          onClick={() => {
            setMonth(currentCalendarMonth());
            setWeekStart(startOfWeekUTC(new Date(), weekStartsOn));
          }}
        >
          {t(($) => $.page.today)}
        </Button>
      </div>

      {isEmpty && (
        <p role="status" className="border-b px-3 py-2 text-caption text-muted-foreground">
          {t(($) => $.page.empty_agenda)}
        </p>
      )}

      {view === "agenda" ? (
        <div className="flex flex-1 min-h-0 flex-col gap-2 overflow-y-auto p-3">
          {weekDayKeys.map((dateKey) => {
            const entries = entriesForDay(dateKey, days.get(dateKey), cycles);
            if (entries.length === 0) return null;
            const utcDate = new Date(`${dateKey}T00:00:00Z`);
            return (
              <section key={dateKey} className="border-b pb-2">
                <h2 className="mb-1 text-caption font-medium text-muted-foreground">
                  {utcDate.toLocaleDateString(locale, { weekday: "short", month: "short", day: "numeric", timeZone: "UTC" })}
                </h2>
                <div className="flex flex-col gap-0.5">
                  {entries.map((entry) => (
                    <EntryChip key={entry.key} entry={entry} onOpenEvent={setOpenEventId} />
                  ))}
                </div>
              </section>
            );
          })}
        </div>
      ) : (
        <div className="flex flex-1 min-h-0 flex-col overflow-auto">
          <div className="grid shrink-0 grid-cols-7 border-b bg-muted/20">
            {(isMonth ? monthCells.slice(0, 7) : weekDayKeys.map((k) => ({ utcDate: new Date(`${k}T00:00:00Z`) }))).map(
              (cell, i) => (
                <div key={i} className="px-2 py-1 text-caption font-medium text-muted-foreground">
                  {cell.utcDate.toLocaleDateString(locale, { weekday: "short", timeZone: "UTC" })}
                </div>
              ),
            )}
          </div>
          <div className="grid flex-1 grid-cols-7 border-l">
            {isMonth
              ? monthCells.map((cell) => (
                  <DayCellView
                    key={cell.date}
                    dateKey={cell.date}
                    utcDate={cell.utcDate}
                    isToday={cell.isToday}
                    isWeekend={cell.isWeekend}
                    inMonth={cell.inMonth}
                    entries={entriesForDay(cell.date, days.get(cell.date), cycles)}
                    onOpenEvent={setOpenEventId}
                    overflowLabel={(count) => t(($) => $.page.more, { count })}
                    dayLabel={cell.utcDate.toLocaleDateString(locale, { month: "long", day: "numeric", timeZone: "UTC" })}
                  />
                ))
              : weekDayKeys.map((dateKey) => {
                  const utcDate = new Date(`${dateKey}T00:00:00Z`);
                  return (
                    <DayCellView
                      key={dateKey}
                      dateKey={dateKey}
                      utcDate={utcDate}
                      isToday={utcDayKey(today) === dateKey}
                      isWeekend={utcDate.getUTCDay() === 0 || utcDate.getUTCDay() === 6}
                      inMonth
                      entries={entriesForDay(dateKey, days.get(dateKey), cycles)}
                      onOpenEvent={setOpenEventId}
                      overflowLabel={(count) => t(($) => $.page.more, { count })}
                      dayLabel={utcDate.toLocaleDateString(locale, { month: "long", day: "numeric", timeZone: "UTC" })}
                    />
                  );
                })}
          </div>
        </div>
      )}

      {dialogTarget && (
        <CalendarEventDialog target={dialogTarget} onClose={() => setDialogTarget(null)} />
      )}

      {openEventId && (
        <CalendarEventSheet
          eventId={openEventId}
          onClose={() => setOpenEventId(null)}
          onEdit={() => {
            if (editingEvent) {
              setDialogTarget({ mode: "edit", event: editingEvent });
              setOpenEventId(null);
            }
          }}
        />
      )}
    </div>
  );
}
