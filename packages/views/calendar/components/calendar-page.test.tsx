// @vitest-environment jsdom
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { CalendarAgenda, CalendarEventEntry } from "@multica/core/types";
import en from "../../locales/en/calendar-events.json";
import { renderWithI18n } from "../../test/i18n";
import { CalendarPage } from "./calendar-page";

// The month GRID matrix (leading/trailing days, weekend, today) is tested in
// packages/core/issues/calendar-grid.test.ts and the day-bucketing pure
// helpers in packages/core/calendar-events/helpers.test.ts. This suite keeps
// the happy path: an event/issue/cycle land in the right cells, the sheet
// opens and can record a response, the new-event dialog submits the right
// body, and Find a slot fills the time fields.

vi.mock("../../navigation", () => ({
  AppLink: ({ children, href }: { children: React.ReactNode; href: string }) => (
    <a href={href}>{children}</a>
  ),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return { ...actual, useWorkspacePaths: () => actual.paths.workspace("acme") };
});
vi.mock("../../common/use-viewing-timezone", () => ({ useViewingTimezone: () => "UTC" }));
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid="actor-avatar" data-actor-id={actorId} />
  ),
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: { user: { id: string; name: string } }) => unknown) =>
    selector({ user: { id: "u-1", name: "Me" } }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({
    queryKey: ["members"],
    queryFn: async () => [
      { user_id: "u-1", name: "Me", email: "me@example.test" },
      { user_id: "u-2", name: "Ada", email: "ada@example.test" },
    ],
  }),
  agentListOptions: () => ({
    queryKey: ["agents"],
    queryFn: async () => [{ id: "a-1", name: "Bot", archived_at: null }],
  }),
}));

const EVENT: CalendarEventEntry = {
  id: "evt-1",
  title: "Design review",
  description: "",
  starts_at: "2026-09-10T15:00:00Z",
  ends_at: "2026-09-10T15:30:00Z",
  all_day: false,
  timezone: "UTC",
  location: "",
  issue_id: null,
  project_id: null,
  status: "scheduled",
  created_by: { type: "member", id: "u-1", name: "Me" },
  source: "vigil",
  decision_id: null,
  participants: [{ type: "member", id: "u-1", name: "Me", response: "pending", required: true }],
  created_at: "2026-09-01T00:00:00Z",
  updated_at: "2026-09-01T00:00:00Z",
};

const AGENDA: CalendarAgenda = {
  from: "2026-08-30T00:00:00Z",
  to: "2026-10-11T00:00:00Z",
  events: [EVENT],
  issues_due: [
    { id: "iss-1", identifier: "MUL-1", title: "Ship it", status: "todo", due_date: "2026-09-12" },
  ],
  cycles: [{ id: "cyc-1", name: "Cycle 14", start_date: "2026-09-01", end_date: "2026-09-14" }],
  meetings: [],
};

const mocks = vi.hoisted(() => ({
  create: vi.fn(async (data: unknown) => ({ ...({} as Record<string, unknown>), id: "new-event", ...(data as object) })),
  respond: vi.fn(async (_v: { id: string; response: string }) => undefined),
}));

vi.mock("@multica/core/calendar-events", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/calendar-events")>();
  return {
    ...actual,
    calendarAgendaOptions: () => ({ queryKey: ["agenda"], queryFn: async () => AGENDA }),
    calendarEventDetailOptions: (_wsId: string, id: string) => ({
      queryKey: ["event-detail", id],
      queryFn: async () => (id === EVENT.id ? EVENT : { ...EVENT, id }),
    }),
    calendarSlotsOptions: (
      _wsId: string,
      participants: string,
      _duration: number,
      _from: string,
      _to: string,
      _tz: string,
    ) => ({
      queryKey: ["slots", participants],
      queryFn: async () => ({
        slots: [{ starts_at: "2026-09-16T10:00:00.000Z", ends_at: "2026-09-16T10:30:00.000Z" }],
        duration_minutes: 30,
        tz: "UTC",
      }),
    }),
    useCreateCalendarEvent: () => ({
      mutate: (data: unknown, opts?: { onSuccess?: (e: unknown) => void }) =>
        mocks.create(data).then((e) => opts?.onSuccess?.(e)),
      isPending: false,
    }),
    useUpdateCalendarEvent: () => ({ mutate: vi.fn(), isPending: false }),
    useCancelCalendarEvent: () => ({ mutate: vi.fn(), isPending: false }),
    useRespondCalendarEvent: () => ({
      mutate: (v: { id: string; response: "accepted" | "declined" | "tentative" }, opts?: { onSuccess?: (e: unknown) => void }) =>
        mocks.respond(v).then(() => opts?.onSuccess?.(EVENT)),
      isPending: false,
    }),
  };
});

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={client}>
      <CalendarPage />
    </QueryClientProvider>,
  );
}

function cellFor(date: string): HTMLElement {
  const cell = document.querySelector<HTMLElement>(`[data-testid="calendar-cell"][data-date="${date}"]`);
  if (!cell) throw new Error(`no calendar cell for ${date}`);
  return cell;
}

beforeAll(() => {
  // Fake only `Date` (not setTimeout/setInterval): the page fires real async
  // queries under test, and faking every timer would freeze the microtask
  // polling `waitFor`/`findBy*` rely on to ever resolve them.
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-09-15T12:00:00Z"));
});
afterAll(() => vi.useRealTimers());
afterEach(cleanup);

describe("CalendarPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders a month with an event, an issue due and a cycle in their cells", async () => {
    renderPage();

    expect(await screen.findByText("September 2026")).toBeTruthy();
    await screen.findByText("Design review");
    expect(cellFor("2026-09-10").textContent).toContain("Design review");
    expect(cellFor("2026-09-12").textContent).toContain("Ship it");
    // The cycle spans 09-01..09-14: it shows on every day in that range,
    // not only the day it starts.
    expect(cellFor("2026-09-05").textContent).toContain("Cycle 14");
    expect(cellFor("2026-09-10").textContent).toContain("Cycle 14");
  });

  it("opens the event sheet and records a response", async () => {
    renderPage();
    await screen.findByText("September 2026");

    fireEvent.click(await screen.findByRole("button", { name: "Design review" }));
    expect(await screen.findByRole("heading", { name: "Design review" })).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: en.sheet.respond_accept }));
    await waitFor(() =>
      expect(mocks.respond).toHaveBeenCalledWith({ id: "evt-1", response: "accepted" }),
    );
  });

  it("submits the new-event dialog with the right body", async () => {
    renderPage();
    await screen.findByText("September 2026");

    fireEvent.click(screen.getByRole("button", { name: en.page.new_event }));
    fireEvent.change(await screen.findByPlaceholderText(en.form.title_placeholder), {
      target: { value: "Standup" },
    });
    fireEvent.click(screen.getByRole("button", { name: en.form.create }));

    await waitFor(() => expect(mocks.create).toHaveBeenCalled());
    const body = mocks.create.mock.calls[0]![0] as { title: string; starts_at: string; ends_at: string };
    expect(body.title).toBe("Standup");
    expect(Date.parse(body.starts_at)).toBeLessThan(Date.parse(body.ends_at));
  });

  it("finds a slot and fills the start/end fields", async () => {
    renderPage();
    await screen.findByText("September 2026");

    fireEvent.click(screen.getByRole("button", { name: en.page.new_event }));
    await screen.findByPlaceholderText(en.form.title_placeholder);

    // Pick a participant so Find a slot is enabled.
    fireEvent.click(screen.getByText(en.form.add_participants));
    fireEvent.click(await screen.findByText("Ada"));

    fireEvent.click(screen.getByRole("button", { name: en.form.find_slot }));
    const useSlot = await screen.findByRole("button", { name: /sep 16/i });
    fireEvent.click(useSlot);

    const startInput = screen.getByLabelText(new RegExp(en.form.starts_at, "i")) as HTMLInputElement;
    expect(startInput.value).toBe("2026-09-16T10:00");
    const endInput = screen.getByLabelText(new RegExp(en.form.ends_at, "i")) as HTMLInputElement;
    expect(endInput.value).toBe("2026-09-16T10:30");
  });
});
