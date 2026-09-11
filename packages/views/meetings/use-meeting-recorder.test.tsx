// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook } from "@testing-library/react";
import { toast } from "sonner";
import { useMeetingRecorderStore } from "@multica/core/meetings/store";

// Lifecycle only. MediaRecorder and getUserMedia do not exist in jsdom (see
// the hook's own note); these cases never reach them.

vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return { ...actual, useWorkspacePaths: () => actual.paths.workspace("acme") };
});
vi.mock("@multica/core/meetings/mutations", () => {
  const mutation = () => ({ mutateAsync: vi.fn() });
  return {
    useAppendMeetingSegment: mutation,
    useAppendMeetingTextSegment: mutation,
    useCreateMeeting: mutation,
    useFinishMeeting: mutation,
  };
});
vi.mock("../navigation", () => ({ useNavigation: () => ({ push: vi.fn() }) }));
vi.mock("../i18n", () => ({ useT: () => ({ t: () => "" }) }));

import { useMeetingRecorder } from "./use-meeting-recorder";

afterEach(() => {
  cleanup();
  useMeetingRecorderStore.setState({ phase: "idle", meetingId: null, startedAt: null, openNonce: 0, stopNonce: 0 });
  vi.mocked(toast.error).mockClear();
});

describe("useMeetingRecorder lifecycle", () => {
  // Regression: logout unmounts the shell mid-recording. The store stayed
  // "recording", so the next session showed a dead pill and start() refused
  // every new recording until a reload.
  it("returns the store to idle when the recorder unmounts", () => {
    const { unmount } = renderHook(() => useMeetingRecorder());
    useMeetingRecorderStore.getState().started("meeting-1", "2026-09-11T09:00:00Z", true);

    unmount();

    expect(useMeetingRecorderStore.getState().phase).toBe("idle");
    expect(useMeetingRecorderStore.getState().meetingId).toBeNull();
  });

  // A nonce bumped before this mount is an old request, not a new one: a
  // remounted shell must not start recording on its own.
  it("ignores a start request made before it mounted", () => {
    useMeetingRecorderStore.getState().open();

    renderHook(() => useMeetingRecorder());

    expect(toast.error).not.toHaveBeenCalled();
    expect(useMeetingRecorderStore.getState().phase).toBe("idle");
  });

  it("still starts on a request made after it mounted", () => {
    renderHook(() => useMeetingRecorder());

    act(() => useMeetingRecorderStore.getState().open());

    // jsdom has no MediaRecorder: reaching start() is what the toast proves.
    expect(toast.error).toHaveBeenCalledTimes(1);
  });
});
