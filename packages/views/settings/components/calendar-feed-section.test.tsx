// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import type { CalendarFeed } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { CalendarFeedSection } from "./calendar-feed-section";

// URL validation itself lives on the server (it is what actually fetches);
// this suite owns the wiring: what is sent, when each button is live, and how
// a broken feed is reported.

const data = vi.hoisted(() => ({
  feed: { url: "", last_error: "" } as CalendarFeed,
  save: vi.fn(async (_url: string) => undefined),
  remove: vi.fn(async () => undefined),
  upcoming: vi.fn(async (_within?: string) => ({ events: [{}, {}], configured: true })),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: { calendarUpcoming: data.upcoming },
}));

vi.mock("@multica/core/calendar/queries", () => ({
  calendarFeedOptions: () => ({
    queryKey: ["calendar", "ws-1", "feed"],
    queryFn: async () => data.feed,
  }),
  useSetCalendarFeed: () => ({ mutateAsync: data.save, isPending: false }),
  useDeleteCalendarFeed: () => ({ mutateAsync: data.remove, isPending: false }),
}));

function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={client}>
      <CalendarFeedSection />
    </QueryClientProvider>,
  );
}

function field(): HTMLInputElement {
  return screen.getByRole("textbox", { name: /calendar feed url/i }) as HTMLInputElement;
}

describe("CalendarFeedSection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    data.feed = { url: "", last_error: "" };
  });

  it("saves a pasted URL and trims it", async () => {
    render();
    fireEvent.change(await screen.findByRole("textbox", { name: /calendar feed url/i }), {
      target: { value: "  webcal://cal.example.test/f.ics  " },
    });
    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));
    await waitFor(() =>
      expect(data.save).toHaveBeenCalledWith("webcal://cal.example.test/f.ics"),
    );
  });

  it("keeps Save inert until the value differs from the server's", async () => {
    data.feed = { url: "https://cal.example.test/f.ics", last_error: "" };
    render();
    await waitFor(() => expect(field().value).toBe("https://cal.example.test/f.ics"));
    expect(screen.getByRole("button", { name: /^save$/i })).toHaveProperty("disabled", true);
    fireEvent.change(field(), { target: { value: "https://cal.example.test/other.ics" } });
    expect(screen.getByRole("button", { name: /^save$/i })).toHaveProperty("disabled", false);
  });

  it("checks the saved feed and reports how many events it holds", async () => {
    data.feed = { url: "https://cal.example.test/f.ics", last_error: "" };
    render();
    await waitFor(() => expect(field().value).toBe("https://cal.example.test/f.ics"));
    fireEvent.click(screen.getByRole("button", { name: /check feed/i }));
    expect(await screen.findByText(/2 events in the next two weeks/i)).toBeTruthy();
    // The window asked for is the server's two-week cap, not the 30m default.
    expect(data.upcoming).toHaveBeenCalledWith("336h");
  });

  it("reports the reason a check failed", async () => {
    data.feed = { url: "https://cal.example.test/f.ics", last_error: "" };
    data.upcoming.mockRejectedValueOnce(
      new ApiError("the calendar feed answered 404", 502, "Bad Gateway", {
        code: "calendar_feed_failed",
      }),
    );
    render();
    await waitFor(() => expect(field().value).toBe("https://cal.example.test/f.ics"));
    fireEvent.click(screen.getByRole("button", { name: /check feed/i }));
    expect(await screen.findByText(/answered 404/i)).toBeTruthy();
  });

  // A feed that stopped working must not look like a calendar with nothing in
  // it, so the last failure is shown without anyone pressing anything.
  it("surfaces the last automatic failure", async () => {
    data.feed = { url: "https://cal.example.test/f.ics", last_error: "could not reach the calendar feed" };
    render();
    expect(await screen.findByText(/could not reach the calendar feed/i)).toBeTruthy();
  });

  it("offers Remove only once something is subscribed", async () => {
    render();
    await screen.findByRole("textbox", { name: /calendar feed url/i });
    expect(screen.queryByRole("button", { name: /^remove$/i })).toBeNull();

    data.feed = { url: "https://cal.example.test/f.ics", last_error: "" };
    render();
    fireEvent.click(await screen.findByRole("button", { name: /^remove$/i }));
    await waitFor(() => expect(data.remove).toHaveBeenCalled());
  });

  it("cannot check a URL the server has not accepted yet", async () => {
    data.feed = { url: "https://cal.example.test/f.ics", last_error: "" };
    render();
    await waitFor(() => expect(field().value).toBe("https://cal.example.test/f.ics"));
    fireEvent.change(field(), { target: { value: "https://cal.example.test/edited.ics" } });
    expect(screen.getByRole("button", { name: /check feed/i })).toHaveProperty("disabled", true);
    expect(data.upcoming).not.toHaveBeenCalled();
  });
});
