// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";

vi.mock("@multica/core/brain/mutations", () => ({
  useCaptureText: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useCaptureUpload: () => ({ mutateAsync: vi.fn(), isPending: false, error: null }),
}));

import { CaptureComposer } from "./capture-composer";

const track = { stop: vi.fn() };
const recorders: FakeRecorder[] = [];

class FakeRecorder {
  state: "inactive" | "recording" = "inactive";
  ondataavailable: ((event: { data: Blob }) => void) | null = null;
  onstop: (() => void) | null = null;
  constructor() {
    recorders.push(this);
  }
  start() {
    this.state = "recording";
  }
  stop = vi.fn(() => {
    this.state = "inactive";
    this.onstop?.();
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
  recorders.length = 0;
  track.stop.mockClear();
});

describe("CaptureComposer voice memo", () => {
  // Regression: switching the Brain tab mid-recording unmounted the composer
  // with the recorder still running — the microphone stayed live and the memo
  // was never sent.
  it("stops the recorder and releases the microphone on unmount", async () => {
    vi.stubGlobal("MediaRecorder", FakeRecorder);
    Object.defineProperty(navigator, "mediaDevices", {
      configurable: true,
      value: { getUserMedia: vi.fn().mockResolvedValue({ getTracks: () => [track] }) },
    });
    const file = { capture: vi.fn(), isPending: false, error: "", detail: "", clearError: vi.fn() };
    const { unmount } = renderWithI18n(<CaptureComposer wsId="ws-1" file={file} />);

    fireEvent.click(await screen.findByRole("button", { name: "Record a voice memo" }));
    await screen.findByRole("button", { name: "Stop recording" });

    unmount();

    expect(recorders[0]?.stop).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(track.stop).toHaveBeenCalled());
  });

  // Regression: every getUserMedia failure (permission refused, no device,
  // device busy) collapsed into one generic "the capture failed" with no
  // cause or action. The DOMException's `name` already says which; use it.
  it.each([
    ["NotAllowedError", "Microphone access was denied. Allow it in your browser settings and try again."],
    ["NotFoundError", "No microphone was found. Connect one and try again."],
    ["NotReadableError", "The microphone could not be read. Close other apps using it and try again."],
  ])("explains a %s getUserMedia failure instead of a generic message", async (name, expected) => {
    // Record button feature-detects both APIs (capture-composer.tsx ~l.81);
    // MediaRecorder just needs to exist, getUserMedia is what rejects.
    vi.stubGlobal("MediaRecorder", FakeRecorder);
    Object.defineProperty(navigator, "mediaDevices", {
      configurable: true,
      value: { getUserMedia: vi.fn().mockRejectedValue(new DOMException("denied", name)) },
    });
    const file = { capture: vi.fn(), isPending: false, error: "", detail: "", clearError: vi.fn() };
    renderWithI18n(<CaptureComposer wsId="ws-1" file={file} />);

    fireEvent.click(await screen.findByRole("button", { name: "Record a voice memo" }));

    expect(await screen.findByText(expected)).toBeInTheDocument();
    expect(screen.queryByText("The capture failed")).not.toBeInTheDocument();
  });
});
