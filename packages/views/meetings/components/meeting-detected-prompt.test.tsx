// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach } from "vitest";
import { act, fireEvent, screen } from "@testing-library/react";
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
  });

  it("renders nothing until a meeting is detected", () => {
    renderWithI18n(<MeetingDetectedPrompt />);
    expect(screen.queryByText("Meeting detected")).toBeNull();
  });

  it("titles the dialog per detected kind and names the app", async () => {
    renderWithI18n(<MeetingDetectedPrompt />);
    detect("huddle", "Slack");
    expect(await screen.findByText("Huddle detected")).toBeTruthy();
    expect(
      screen.getByText("Slack is using the microphone. Take notes?"),
    ).toBeTruthy();
  });

  it("starts the recorder with the app name when the user takes notes", async () => {
    renderWithI18n(<MeetingDetectedPrompt />);
    detect("meeting", "Zoom");
    fireEvent.click(await screen.findByText("Take notes"));
    expect(data.store.open).toHaveBeenCalledWith({
      title: "Zoom meeting",
      appName: "Zoom",
    });
  });

  it("dismisses without recording", async () => {
    renderWithI18n(<MeetingDetectedPrompt />);
    detect("call", "FaceTime");
    expect(await screen.findByText("Call detected")).toBeTruthy();
    fireEvent.click(screen.getByText("Not now"));
    expect(data.store.open).not.toHaveBeenCalled();
    expect(screen.queryByText("Call detected")).toBeNull();
  });

  it("stays closed while a recording is already running", () => {
    data.store.phase = "recording";
    renderWithI18n(<MeetingDetectedPrompt />);
    detect("meeting", "Zoom");
    expect(screen.queryByText("Meeting detected")).toBeNull();
  });

  it("unsubscribes on unmount", () => {
    const { unmount } = renderWithI18n(<MeetingDetectedPrompt />);
    unmount();
    expect(data.unsubscribe).toHaveBeenCalled();
  });
});
