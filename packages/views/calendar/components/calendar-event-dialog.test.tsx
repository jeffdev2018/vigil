// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { CalendarEventEntry } from "@multica/core/types";
import en from "../../locales/en/calendar-events.json";
import { renderWithI18n } from "../../test/i18n";
import { CalendarEventDialog } from "./calendar-event-dialog";

// This dialog's DialogTitle already branched correctly on target.mode, but
// the sr-only DialogDescription stayed hardcoded to the create-mode copy
// (MUL audit finding, P2: reproducible on every edit-mode open, a screen
// reader user is told they are creating a NEW event while editing an
// existing one).

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("../../common/use-viewing-timezone", () => ({ useViewingTimezone: () => "UTC" }));
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));
vi.mock("../../modals/issue-picker-modal", () => ({
  IssuePickerModal: () => null,
}));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: async () => [] }),
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: async () => [] }),
}));
vi.mock("@multica/core/calendar-events", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/calendar-events")>();
  return {
    ...actual,
    calendarSlotsOptions: () => ({ queryKey: ["slots"], queryFn: async () => ({ slots: [] }) }),
    useCreateCalendarEvent: () => ({ mutate: vi.fn(), isPending: false }),
    useUpdateCalendarEvent: () => ({ mutate: vi.fn(), isPending: false }),
  };
});

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
  participants: [],
  created_at: "2026-09-01T00:00:00Z",
  updated_at: "2026-09-01T00:00:00Z",
};

function renderDialog(target: Parameters<typeof CalendarEventDialog>[0]["target"]) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={client}>
      <CalendarEventDialog target={target} onClose={() => {}} />
    </QueryClientProvider>,
  );
}

afterEach(cleanup);

describe("CalendarEventDialog", () => {
  it("announces a new event in create mode", () => {
    renderDialog({ mode: "create" });

    expect(screen.getByText(en.form.create_title, { selector: "h2" })).toBeTruthy();
    expect(document.querySelector('[class*="sr-only"]')?.textContent).toBe(
      en.form.create_title,
    );
  });

  it("announces editing the existing event in edit mode, not creating a new one", () => {
    renderDialog({ mode: "edit", event: EVENT });

    expect(screen.getByText(en.form.edit_title, { selector: "h2" })).toBeTruthy();
    const description = document.querySelector('[class*="sr-only"]');
    expect(description?.textContent).toBe(en.form.edit_title);
    expect(description?.textContent).not.toBe(en.form.create_title);
  });
});
