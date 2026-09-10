// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import { renderWithI18n } from "../../test/i18n";
import { CalendarPublishSection } from "./calendar-publish-section";

// The URL-minting and Google-import flows are wired here; the outcomes
// themselves (whether a feed URL is valid, whether Google is connected) are
// the server's job and are exercised in the Go handler tests.

const data = vi.hoisted(() => ({
  status: { configured: false } as { configured: boolean; created_at?: string },
  mint: vi.fn(async () => ({ url: "https://vigil.test/api/calendar/ics/mcal_abc", path: "/api/calendar/ics/mcal_abc" })),
  revoke: vi.fn(async () => undefined),
  importGoogle: vi.fn(async (_v: { from?: string; to?: string }) => ({ created: 2, updated: 1, seen: 3 })),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/calendar-events", () => ({
  feedTokenOptions: () => ({
    queryKey: ["calendar-events", "ws-1", "feed-token"],
    queryFn: async () => data.status,
  }),
  useMintCalendarFeedToken: () => ({
    mutate: (_v: undefined, opts?: { onSuccess?: (r: unknown) => void; onError?: (e: unknown) => void }) =>
      data.mint().then((r) => opts?.onSuccess?.(r), (e) => opts?.onError?.(e)),
    isPending: false,
  }),
  useRevokeCalendarFeedToken: () => ({
    mutate: (_v: undefined, opts?: { onSuccess?: () => void; onError?: (e: unknown) => void }) =>
      data.revoke().then(() => opts?.onSuccess?.(), (e) => opts?.onError?.(e)),
    isPending: false,
  }),
  useImportGoogleCalendar: () => ({
    mutate: (
      v: { from?: string; to?: string },
      opts?: { onSuccess?: (r: unknown) => void; onError?: (e: unknown) => void },
    ) => data.importGoogle(v).then((r) => opts?.onSuccess?.(r), (e) => opts?.onError?.(e)),
    isPending: false,
  }),
}));

function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={client}>
      <CalendarPublishSection />
    </QueryClientProvider>,
  );
}

describe("CalendarPublishSection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    data.status = { configured: false };
  });

  it("mints a link and shows it once, with a hint that it will not be shown again", async () => {
    render();
    // Wait for the feed-token query to settle — the mint button stays
    // disabled (`isLoading`) until then, so a click that lands too early is
    // silently swallowed by the disabled button rather than firing.
    await screen.findByText(/not published yet/i);
    fireEvent.click(screen.getByRole("button", { name: /get link/i }));
    await waitFor(() => expect(data.mint).toHaveBeenCalled());
    expect(await screen.findByDisplayValue("https://vigil.test/api/calendar/ics/mcal_abc")).toBeTruthy();
    expect(screen.getByText(/shown once/i)).toBeTruthy();
  });

  it("offers Rotate and Revoke once a feed is configured", async () => {
    data.status = { configured: true, created_at: "2026-09-01T00:00:00Z" };
    render();
    expect(await screen.findByRole("button", { name: /rotate link/i })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /^revoke$/i }));
    await waitFor(() => expect(data.revoke).toHaveBeenCalled());
  });

  it("imports from Google and reports the created/updated/seen counts", async () => {
    render();
    fireEvent.click(await screen.findByRole("button", { name: /import from google/i }));
    await waitFor(() => expect(data.importGoogle).toHaveBeenCalled());
    expect(await screen.findByText(/2 created, 1 updated, 3 seen/i)).toBeTruthy();
  });

  it("explains a missing Google connection with a link to Integrations", async () => {
    data.importGoogle.mockRejectedValueOnce(
      new ApiError("connect Google Calendar first", 409, "Conflict", { code: "no_google_connection" }),
    );
    render();
    fireEvent.click(await screen.findByRole("button", { name: /import from google/i }));
    expect(await screen.findByText(/connect google calendar/i)).toBeTruthy();
    expect(screen.getByRole("link", { name: /integrations/i })).toBeTruthy();
  });

  it("explains Composio being off with a 503", async () => {
    data.importGoogle.mockRejectedValueOnce(new ApiError("composio not configured", 503, "Service Unavailable"));
    render();
    fireEvent.click(await screen.findByRole("button", { name: /import from google/i }));
    expect(await screen.findByText(/not configured on this server/i)).toBeTruthy();
  });
});
