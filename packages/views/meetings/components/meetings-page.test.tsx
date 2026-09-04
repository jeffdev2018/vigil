// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach } from "vitest";
import { configStore } from "@multica/core/config";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { MeetingListResponse } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { MeetingsPage } from "./meetings-page";

// The recorder itself (MediaRecorder, getUserMedia, getDisplayMedia,
// AudioContext) is browser-only and has no jsdom equivalent, so it is not
// exercised here — faking all four would test the fakes. Its parsing and
// error-code branches are covered in packages/core/meetings/meetings.test.ts.

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({
    meetings: () => "/acme/meetings",
    meetingDetail: (id: string) => `/acme/meetings/${id}`,
  }),
}));

const data = vi.hoisted(() => ({
  meetings: { meetings: [] } as MeetingListResponse,
  deleteMeeting: vi.fn(async (_id: string) => undefined),
  store: {
    phase: "idle" as string,
    sttUnavailable: false,
    open: vi.fn(),
  },
}));

const MEETINGS: { rows: MeetingListResponse["meetings"] } = vi.hoisted(() => ({
  rows: [
    {
      id: "meet-1",
      title: "Weekly sync",
      app_name: "Zoom",
      status: "done",
      transcript: "",
      summary_markdown: "",
      segment_count: 6,
      created_by: "user-1",
      started_at: "2026-01-01T09:00:00Z",
      ended_at: "2026-01-01T09:30:00Z",
      actions: [],
      summary_unavailable: false,
      action_count: 0,
      can_manage: true,
    },
    {
      id: "meet-2",
      title: "Design review",
      app_name: "",
      status: "recording",
      transcript: "",
      summary_markdown: "",
      segment_count: 1,
      created_by: "user-1",
      started_at: "2026-01-01T10:00:00Z",
      actions: [],
      summary_unavailable: false,
      action_count: 0,
      can_manage: false,
    },
  ],
}));

vi.mock("@multica/core/meetings/mutations", () => ({
  useDeleteMeeting: () => ({
    mutateAsync: (id: string) => data.deleteMeeting(id),
    isPending: false,
  }),
}));

vi.mock("@multica/core/meetings/queries", () => ({
  meetingListOptions: () => ({
    queryKey: ["meetings", "ws-1", "list"],
    queryFn: async () => data.meetings,
  }),
}));

// Zustand callable-store shape: selector call plus getState.
vi.mock("@multica/core/meetings/store", () => {
  const useMeetingRecorderStore = <T,>(selector: (s: unknown) => T): T =>
    selector(data.store);
  useMeetingRecorderStore.getState = () => data.store;
  return {
    useMeetingRecorderStore,
    openMeetingRecorder: (...args: unknown[]) => data.store.open(...args),
  };
});

function makeAdapter(): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/meetings",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
    openInNewTab: vi.fn(),
  };
}

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const adapter = makeAdapter();
  renderWithI18n(
    <QueryClientProvider client={client}>
      <NavigationProvider value={adapter}>
        <MeetingsPage />
      </NavigationProvider>
    </QueryClientProvider>,
  );
  return adapter;
}

describe("MeetingsPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    data.store.phase = "idle";
    data.store.sttUnavailable = false;
    // The server declares its transcription provider in /api/config.
    configStore.getState().setMeetingTranscriptionAvailable(true);
    data.meetings.meetings = [...MEETINGS.rows];
  });

  it("renders one row per meeting with app name and segment count", async () => {
    renderPage();
    expect(await screen.findByText("Weekly sync")).toBeTruthy();
    expect(screen.getByText("Design review")).toBeTruthy();
    expect(screen.getByText("Zoom")).toBeTruthy();
    // A meeting with no app name falls back to the localized placeholder.
    expect(screen.getByText("No app")).toBeTruthy();
    expect(screen.getByText("6 segments")).toBeTruthy();
  });

  it("the header action asks the shell recorder to start", async () => {
    renderPage();
    await screen.findByText("Weekly sync");
    fireEvent.click(screen.getByRole("button", { name: /record a meeting/i }));
    expect(data.store.open).toHaveBeenCalled();
  });

  it("shows the empty state when the workspace has no meetings", async () => {
    data.meetings.meetings = [];
    renderPage();
    expect(await screen.findByText("No meetings yet")).toBeTruthy();
  });

  it("shows a quiet banner and disables recording when the server has no STT", async () => {
    data.store.sttUnavailable = true;
    renderPage();
    // A capability notice, not an error: the list still renders.
    expect(await screen.findByText("Weekly sync")).toBeTruthy();
    expect(screen.getByText("Transcription is not configured")).toBeTruthy();
    const record = screen.getByRole("button", { name: /record a meeting/i });
    expect(record.hasAttribute("disabled")).toBe(true);
  });

  it("hides recording up front when /api/config declares no transcription provider", async () => {
    configStore.getState().setMeetingTranscriptionAvailable(false);
    renderPage();
    expect(await screen.findByText("Weekly sync")).toBeTruthy();
    expect(screen.getByText("Transcription is not configured")).toBeTruthy();
    expect(
      screen.getByRole("button", { name: /record a meeting/i }).hasAttribute("disabled"),
    ).toBe(true);
  });

  it("only a meeting the viewer can manage offers the row actions menu", async () => {
    renderPage();
    await screen.findByText("Weekly sync");
    expect(screen.getByRole("button", { name: /actions for weekly sync/i })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /actions for design review/i })).toBeNull();
  });

  it("deleting a meeting confirms first, then calls the server", async () => {
    renderPage();
    await screen.findByText("Weekly sync");
    fireEvent.click(screen.getByRole("button", { name: /actions for weekly sync/i }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /delete meeting/i }));
    // Nothing is sent until the confirm step is answered.
    expect(data.deleteMeeting).not.toHaveBeenCalled();
    expect(await screen.findByText(/delete .weekly sync/i)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /^delete$/i }));
    expect(data.deleteMeeting).toHaveBeenCalledWith("meet-1");
  });

  it("clicking a row navigates to that meeting's detail page", async () => {
    const adapter = renderPage();
    fireEvent.click(await screen.findByText("Weekly sync"));
    // The whole row is the click target (useRowLink), so the name cell stays
    // plain text rather than a nested anchor.
    expect(adapter.push).toHaveBeenCalledWith("/acme/meetings/meet-1");
  });
});
