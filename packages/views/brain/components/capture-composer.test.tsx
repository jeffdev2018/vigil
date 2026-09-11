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
});
