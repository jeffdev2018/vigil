// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  buildCalendarGrid,
  calendarMonthIsEmpty,
  CALENDAR_CELL_COUNT,
  currentCalendarMonth,
  shiftMonth,
  utcDateKey,
} from "./calendar-grid";

// The calendar view's whole layout contract lives here — the component suite
// keeps the happy path, the overflow popover and the empty state, and points
// back at this file for the grid matrix.

interface Row {
  id: string;
  due_date: string | null;
}

const dueDate = (row: Row) => row.due_date;

describe("buildCalendarGrid", () => {
  it("always returns 42 cells so the calendar never changes height", () => {
    for (const month of [1, 2, 6, 12]) {
      const cells = buildCalendarGrid({ year: 2026, month }, [], dueDate);
      expect(cells).toHaveLength(CALENDAR_CELL_COUNT);
    }
  });

  it("starts on the Sunday on or before the first of the month", () => {
    // 2026-03-01 is a Sunday, so March needs no leading days.
    const march = buildCalendarGrid({ year: 2026, month: 3 }, [], dueDate);
    expect(march[0]!.date).toBe("2026-03-01");
    // 2026-04-01 is a Wednesday: the grid backs up to Sunday 2026-03-29.
    const april = buildCalendarGrid({ year: 2026, month: 4 }, [], dueDate);
    expect(april[0]!.date).toBe("2026-03-29");
    expect(april[0]!.inMonth).toBe(false);
    expect(april[3]!.date).toBe("2026-04-01");
    expect(april[3]!.inMonth).toBe(true);
  });

  it("honours a Monday-first week", () => {
    const april = buildCalendarGrid({ year: 2026, month: 4 }, [], dueDate, { weekStartsOn: 1 });
    expect(april[0]!.date).toBe("2026-03-30");
    expect(april[0]!.weekday).toBe(1);
  });

  it("places an issue on its UTC due date, not the viewer's local day", () => {
    // The classic failure: a "YYYY-MM-DD" parsed at LOCAL midnight lands on the
    // previous day for every viewer west of UTC and on the next for some east
    // of it. This assertion is timezone-independent BY CONSTRUCTION — the grid
    // never touches the local calendar — which is what makes running the suite
    // under any TZ meaningless as a source of flake.
    const cells = buildCalendarGrid(
      { year: 2026, month: 3 },
      [{ id: "a", due_date: "2026-03-01" }],
      dueDate,
    );
    const first = cells.find((c) => c.date === "2026-03-01")!;
    expect(first.items.map((i) => i.id)).toEqual(["a"]);
    expect(cells.filter((c) => c.items.length > 0)).toHaveLength(1);
  });

  it("keeps several issues on the same day in input order", () => {
    const cells = buildCalendarGrid(
      { year: 2026, month: 3 },
      [
        { id: "a", due_date: "2026-03-10" },
        { id: "b", due_date: "2026-03-10" },
        { id: "c", due_date: "2026-03-11" },
      ],
      dueDate,
    );
    expect(cells.find((c) => c.date === "2026-03-10")!.items.map((i) => i.id)).toEqual(["a", "b"]);
    expect(cells.find((c) => c.date === "2026-03-11")!.items.map((i) => i.id)).toEqual(["c"]);
  });

  it("drops issues with no date and issues outside the rendered window", () => {
    const cells = buildCalendarGrid(
      { year: 2026, month: 3 },
      [
        { id: "undated", due_date: null },
        { id: "unparseable", due_date: "not a date" },
        { id: "far", due_date: "2027-08-04" },
      ],
      dueDate,
    );
    expect(cells.every((c) => c.items.length === 0)).toBe(true);
  });

  it("still shows an issue that falls in a neighbouring month's cell", () => {
    // 2026-04's grid starts 2026-03-29, so an issue due then is visible even
    // though it belongs to March. That is context, not noise.
    const cells = buildCalendarGrid(
      { year: 2026, month: 4 },
      [{ id: "spill", due_date: "2026-03-30" }],
      dueDate,
    );
    const cell = cells.find((c) => c.date === "2026-03-30")!;
    expect(cell.inMonth).toBe(false);
    expect(cell.items.map((i) => i.id)).toEqual(["spill"]);
  });

  it("marks weekends and today", () => {
    const cells = buildCalendarGrid({ year: 2026, month: 3 }, [], dueDate, {
      today: new Date(Date.UTC(2026, 2, 12)),
    });
    expect(cells.find((c) => c.date === "2026-03-07")!.isWeekend).toBe(true);
    expect(cells.find((c) => c.date === "2026-03-09")!.isWeekend).toBe(false);
    const todayCells = cells.filter((c) => c.isToday);
    expect(todayCells.map((c) => c.date)).toEqual(["2026-03-12"]);
  });

  it("marks nothing as today when today is outside the rendered window", () => {
    const cells = buildCalendarGrid({ year: 2026, month: 3 }, [], dueDate, {
      today: new Date(Date.UTC(2030, 0, 1)),
    });
    expect(cells.some((c) => c.isToday)).toBe(false);
  });
});

describe("calendarMonthIsEmpty", () => {
  it("is true when only the neighbouring-month cells hold issues", () => {
    const cells = buildCalendarGrid(
      { year: 2026, month: 4 },
      [{ id: "spill", due_date: "2026-03-30" }],
      dueDate,
    );
    expect(calendarMonthIsEmpty(cells)).toBe(true);
  });

  it("is false as soon as one in-month cell holds an issue", () => {
    const cells = buildCalendarGrid(
      { year: 2026, month: 4 },
      [{ id: "real", due_date: "2026-04-02" }],
      dueDate,
    );
    expect(calendarMonthIsEmpty(cells)).toBe(false);
  });
});

describe("shiftMonth", () => {
  it("crosses year boundaries in both directions", () => {
    expect(shiftMonth({ year: 2026, month: 12 }, 1)).toEqual({ year: 2027, month: 1 });
    expect(shiftMonth({ year: 2026, month: 1 }, -1)).toEqual({ year: 2025, month: 12 });
    expect(shiftMonth({ year: 2026, month: 6 }, -18)).toEqual({ year: 2024, month: 12 });
  });
});

describe("currentCalendarMonth", () => {
  it("reads the month in UTC", () => {
    // 23:30 UTC on Mar 31 is already April 1 for a viewer in UTC+13. The grid
    // buckets by UTC, so the month it opens on has to be UTC's too.
    expect(currentCalendarMonth(new Date("2026-03-31T23:30:00Z"))).toEqual({
      year: 2026,
      month: 3,
    });
  });
});

describe("utcDateKey", () => {
  it("zero-pads so keys sort and compare as strings", () => {
    expect(utcDateKey(new Date(Date.UTC(2026, 0, 5)))).toBe("2026-01-05");
  });
});
