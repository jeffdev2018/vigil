/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeAll, afterAll, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import type { Issue } from "@multica/core/types";
import en from "../../locales/en/issues.json";
import { renderWithI18n } from "../../test/i18n";
import { CalendarView } from "./calendar-view";

// The grid MATRIX is tested in packages/core/issues/calendar-grid.test.ts —
// every leading/trailing-day, weekend, today and bucketing case lives there.
// This suite keeps the happy path, the overflow popover, the empty month and
// the timezone stability of what actually reaches the DOM.

vi.mock("../../navigation", () => ({
  AppLink: ({ children, href }: { children: React.ReactNode; href: string }) => (
    <a href={href}>{children}</a>
  ),
  useNavigation: () => ({ push: vi.fn(), pathname: "/issues" }),
  resolveClickIntent: () => "push",
  useIntentNavigate: () => () => {},
}));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return {
    ...actual,
    useWorkspacePaths: () => actual.paths.workspace("acme"),
  };
});
vi.mock("../actions", () => ({
  IssueActionsContextMenu: ({ children }: { children: React.ReactNode }) => children,
}));

function issue(id: string, dueDate: string | null, title = id): Issue {
  return {
    id,
    identifier: `MUL-${id}`,
    number: 1,
    title,
    description: "",
    status: "todo",
    priority: "medium",
    workspace_id: "ws-1",
    project_id: null,
    parent_issue_id: null,
    assignee_id: null,
    assignee_type: null,
    creator_id: "u-1",
    creator_type: "member",
    labels: [],
    position: 0,
    stage: null,
    start_date: null,
    due_date: dueDate,
    metadata: {},
    properties: {},
    created_at: "2026-03-01T00:00:00Z",
    updated_at: "2026-03-01T00:00:00Z",
  } as unknown as Issue;
}

function cellFor(date: string): HTMLElement {
  const cell = document.querySelector<HTMLElement>(`[data-testid="calendar-cell"][data-date="${date}"]`);
  if (!cell) throw new Error(`no calendar cell for ${date}`);
  return cell;
}

beforeAll(() => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-03-12T12:00:00Z"));
});
afterAll(() => vi.useRealTimers());
afterEach(cleanup);

describe("CalendarView", () => {
  it("opens on the current month and places an issue on its due date", () => {
    renderWithI18n(<CalendarView issues={[issue("a", "2026-03-10", "Ship it")]} />);

    expect(screen.getByText("March 2026")).toBeTruthy();
    expect(cellFor("2026-03-10").textContent).toContain("Ship it");
  });

  // Acceptance 11. The assertion is timezone-independent by construction: the
  // grid never touches the local calendar, so this holds under any TZ. The
  // fixture below is the case that used to break — a date that is the previous
  // day for every viewer west of UTC when parsed at local midnight.
  it("places an issue on its UTC due date, not the viewer's local day", () => {
    renderWithI18n(<CalendarView issues={[issue("a", "2026-03-01", "First of March")]} />);

    expect(cellFor("2026-03-01").textContent).toContain("First of March");
    // The whole grid holds it exactly once — a local-midnight parse would put
    // it in the preceding cell instead.
    expect(
      [...document.querySelectorAll('[data-testid="calendar-cell"]')].filter((cell) =>
        (cell.textContent ?? "").includes("First of March"),
      ),
    ).toHaveLength(1);
  });

  // Acceptance 12.
  it("folds a busy day behind +N and the popover carries the WHOLE day", () => {
    const busy = ["a", "b", "c", "d", "e", "f"].map((id) => issue(id, "2026-03-10", `Issue ${id}`));
    renderWithI18n(<CalendarView issues={busy} />);

    const overflow = screen.getByText("+2 more");
    // The chips before the fold are on screen, the rest are not.
    expect(cellFor("2026-03-10").textContent).toContain("Issue a");
    expect(cellFor("2026-03-10").textContent).not.toContain("Issue f");

    fireEvent.click(overflow);
    // The popover lists every issue of that day, including the ones already
    // shown as chips — re-joining two lists mentally is what it exists to
    // avoid.
    for (const id of ["a", "b", "c", "d", "e", "f"]) {
      expect(screen.getAllByText(`Issue ${id}`).length).toBeGreaterThan(0);
    }
  });

  it("names an empty month instead of leaving an unexplained grid", () => {
    renderWithI18n(<CalendarView issues={[]} />);
    expect(screen.getByRole("status").textContent).toBe(en.calendar.empty_month);
    // The grid stays mounted: the month control is how the user reaches a
    // month that does have something.
    expect(document.querySelectorAll('[data-testid="calendar-cell"]')).toHaveLength(42);
  });

  it("does not call an undated issue an empty month, and drops it from the grid", () => {
    renderWithI18n(<CalendarView issues={[issue("a", null, "No date")]} />);
    // Undated issues have no cell, so the month IS empty and says so.
    expect(screen.getByRole("status")).toBeTruthy();
    expect(screen.queryByText("No date")).toBeNull();
  });

  it("pages to the previous and next month", () => {
    renderWithI18n(<CalendarView issues={[issue("a", "2026-04-02", "April work")]} />);

    // March's 42-cell grid runs to 2026-04-11, so the April issue is already
    // visible in a TRAILING cell — that is the design (an issue due on a
    // neighbouring-month cell is context, not noise), and it is why the month
    // label is what this test navigates by.
    expect(screen.getByText("March 2026")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: en.calendar.next_month }));
    expect(screen.getByText("April 2026")).toBeTruthy();
    expect(cellFor("2026-04-02").textContent).toContain("April work");

    fireEvent.click(screen.getByRole("button", { name: en.calendar.previous_month }));
    expect(screen.getByText("March 2026")).toBeTruthy();
  });
});
