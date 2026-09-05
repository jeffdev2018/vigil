// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach } from "vitest";
import { act, fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { CalendarUpcoming } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { MeetingDetectedPrompt } from "./meeting-detected-prompt";

// The detection state machine itself (debounce, one prompt per mic session,
// bundle matching) is covered in apps/desktop/src/main/meeting-detector.test.ts.
// This suite only owns the DOM side: which title per kind, and what each
// button does.

const data = vi.hoisted(() => ({
  store: { phase: "idle" as string, open: vi.fn() },
  emit: null as null | ((meeting: unknown) => void),
  unsubscribe: vi.fn(),
  upcoming: null as unknown,
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/calendar/queries", () => ({
  calendarUpcomingOptions: () => ({
    queryKey: ["calendar", "ws-1", "upcoming", "30m"],
    queryFn: async () => data.upcoming,
    retry: false,
  }),
}));

vi.mock("../../platform/meeting-detection", () => ({
  subscribeMeetingDetected: (cb: (meeting: unknown) => void) => {
    data.emit = cb;
    return data.unsubscribe;
  },
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

function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <MeetingDetectedPrompt />
    </QueryClientProvider>,
  );
}

function upcoming(...summaries: [string, boolean][]): CalendarUpcoming {
  return {
    configured: true,
    events: summaries.map(([summary, in_progress]) => ({
      summary,
      start: "2026-09-04T09:00:00Z",
      end: "2026-09-04T10:00:00Z",
      in_progress,
    })),
  };
}

function detect(kind: string, appName: string) {
  act(() => {
    data.emit?.({ kind, appName, bundleId: `test.${appName}` });
  });
}

describe("MeetingDetectedPrompt", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    data.store.phase = "idle";
    data.emit = null;
    data.upcoming = { configured: false, events: [] };
  });

  it("renders nothing until a meeting is detected", () => {
    render();
    expect(screen.queryByText("Meeting detected")).toBeNull();
  });

  it("titles the dialog per detected kind and names the app", async () => {
    render();
    detect("huddle", "Slack");
    expect(await screen.findByText("Huddle detected")).toBeTruthy();
    expect(
      screen.getByText("Slack is using the microphone. Take notes?"),
    ).toBeTruthy();
  });

  it("starts the recorder with the app name when the user takes notes", async () => {
    render();
    detect("meeting", "Zoom");
    fireEvent.click(await screen.findByText("Take notes"));
    expect(data.store.open).toHaveBeenCalledWith({
      title: "Zoom meeting",
      appName: "Zoom",
    });
  });

  it("dismisses without recording", async () => {
    render();
    detect("call", "FaceTime");
    expect(await screen.findByText("Call detected")).toBeTruthy();
    fireEvent.click(screen.getByText("Not now"));
    expect(data.store.open).not.toHaveBeenCalled();
    expect(screen.queryByText("Call detected")).toBeNull();
  });

  it("stays closed while a recording is already running", () => {
    data.store.phase = "recording";
    render();
    detect("meeting", "Zoom");
    expect(screen.queryByText("Meeting detected")).toBeNull();
  });

  it("unsubscribes on unmount", () => {
    const { unmount } = render();
    unmount();
    expect(data.unsubscribe).toHaveBeenCalled();
  });

  // The app that took the microphone says "Zoom"; the calendar says what the
  // meeting is called.
  it("names the running calendar event and records under that name", async () => {
    data.upcoming = upcoming(["Later today", false], ["Sprint review", true]);
    render();
    detect("meeting", "Zoom");
    expect(await screen.findByText("Looks like: Sprint review")).toBeTruthy();
    fireEvent.click(screen.getByText("Take notes"));
    expect(data.store.open).toHaveBeenCalledWith({
      title: "Sprint review",
      appName: "Zoom",
    });
  });

  it("keeps the app-based title when nothing is running on the calendar", async () => {
    data.upcoming = upcoming(["Later today", false]);
    render();
    detect("meeting", "Zoom");
    expect(await screen.findByText("Meeting detected")).toBeTruthy();
    expect(screen.queryByText(/Looks like/)).toBeNull();
    fireEvent.click(screen.getByText("Take notes"));
    expect(data.store.open).toHaveBeenCalledWith({
      title: "Zoom meeting",
      appName: "Zoom",
    });
  });
});
