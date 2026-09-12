"use client";

import { useMemo } from "react";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  dateToOffset,
  type RoadmapTimeline,
  type RoadmapTimelineRow,
} from "@multica/core/roadmap";
import { AppLink } from "../../navigation";
import { useLocale, useT } from "../../i18n";

const LABEL_WIDTH = 224;
const ROW_HEIGHT = 36;

interface MonthTick {
  key: string;
  label: string;
  offset: number;
}

// Month gridlines over [rangeStart, rangeEnd]. The core timeline carries
// UTC-midnight Dates, so iterating month starts in UTC keeps every tick on an
// exact day boundary regardless of the viewer's zone.
function monthTicks(
  rangeStart: Date,
  rangeEnd: Date,
  locale: string,
): MonthTick[] {
  const cursor = new Date(
    Date.UTC(rangeStart.getUTCFullYear(), rangeStart.getUTCMonth(), 1),
  );
  const ticks: MonthTick[] = [];
  while (cursor.getTime() <= rangeEnd.getTime()) {
    ticks.push({
      key: cursor.toISOString(),
      label: cursor.toLocaleDateString(locale, {
        month: "short",
        year: "numeric",
        timeZone: "UTC",
      }),
      offset: dateToOffset(cursor, rangeStart, rangeEnd),
    });
    cursor.setUTCMonth(cursor.getUTCMonth() + 1);
  }
  return ticks;
}

// Offset (0-1) to a CSS `left` inside the track that sits right of the fixed
// label column: percent of the REMAINING width, shifted by the column.
function trackLeft(offset: number): string {
  return `calc(${LABEL_WIDTH}px + (100% - ${LABEL_WIDTH}px) * ${offset})`;
}

function TimelineRow({
  row,
  rangeStart,
  rangeEnd,
}: {
  row: RoadmapTimelineRow;
  rangeStart: Date;
  rangeEnd: Date;
}) {
  const { t } = useT("roadmap");
  const paths = useWorkspacePaths();
  const left = dateToOffset(row.startDate, rangeStart, rangeEnd);
  const width = Math.max(
    dateToOffset(row.dueDate, rangeStart, rangeEnd) - left,
    0.004,
  );
  const percent = Math.round(row.progress * 100);

  return (
    <div
      data-testid="project-timeline-row"
      data-project-id={row.project.id}
      className="flex items-center border-b border-border/60"
      style={{ height: ROW_HEIGHT }}
    >
      <div
        className="flex shrink-0 items-baseline gap-2 truncate px-3"
        style={{ width: LABEL_WIDTH }}
      >
        <span className="truncate text-body">{row.project.title}</span>
        <span className="shrink-0 text-caption tabular-nums text-muted-foreground">
          {t(($) => $.timeline.progress, { percent })}
        </span>
      </div>
      <div className="relative h-full flex-1">
        <AppLink
          href={paths.projectDetail(row.project.id)}
          aria-label={row.project.title}
          className="absolute top-1/2 block h-5 -translate-y-1/2 overflow-hidden rounded-md bg-primary/15 transition-colors hover:bg-primary/25"
          style={{ left: `${left * 100}%`, width: `${width * 100}%` }}
        >
          <div
            role="progressbar"
            aria-valuenow={percent}
            aria-valuemin={0}
            aria-valuemax={100}
            className="h-full rounded-md bg-primary/60"
            style={{ width: `${percent}%` }}
          />
        </AppLink>
      </div>
    </div>
  );
}

export function ProjectTimeline({ timeline }: { timeline: RoadmapTimeline }) {
  const { t } = useT("roadmap");
  const locale = useLocale();
  const { rangeStart, rangeEnd, rows } = timeline;

  const months = useMemo(
    () => monthTicks(rangeStart, rangeEnd, locale),
    [rangeStart, rangeEnd, locale],
  );

  // UTC calendar day, same anchoring as the core timeline's range: the marker
  // lands on exactly one day boundary wherever the viewer sits.
  const today = useMemo(() => {
    const now = new Date();
    return new Date(
      Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()),
    );
  }, []);
  const showToday =
    today.getTime() >= rangeStart.getTime() &&
    today.getTime() <= rangeEnd.getTime();

  return (
    <div data-testid="project-timeline" className="relative">
      {/* Month axis */}
      <div className="flex border-b border-border/60">
        <div className="shrink-0" style={{ width: LABEL_WIDTH }} />
        <div className="relative h-8 flex-1">
          {months.map((month) => (
            <div
              key={month.key}
              className="absolute inset-y-0 flex items-center border-l border-border/60 pl-1 text-micro text-muted-foreground"
              style={{ left: `${month.offset * 100}%` }}
            >
              {month.label}
            </div>
          ))}
        </div>
      </div>

      {/* Rows + today marker spanning the whole chart */}
      <div className="relative">
        {showToday && (
          <div
            data-testid="project-timeline-today"
            aria-label={t(($) => $.timeline.today)}
            className="absolute inset-y-0 z-10 w-px bg-brand/70"
            style={{ left: trackLeft(dateToOffset(today, rangeStart, rangeEnd)) }}
          />
        )}
        {rows.map((row) => (
          <TimelineRow
            key={row.project.id}
            row={row}
            rangeStart={rangeStart}
            rangeEnd={rangeEnd}
          />
        ))}
      </div>
    </div>
  );
}
