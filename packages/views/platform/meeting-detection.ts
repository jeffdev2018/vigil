// Desktop-only bridge for ambient meeting detection (macOS).
//
// Same shape as local-directory.ts: read `window.desktopAPI` defensively so
// these can be called from shared views that also render on web, where the
// bridge is absent and every call is a no-op.

export type DetectedMeetingKind = "huddle" | "call" | "meeting";

export interface DetectedMeeting {
  kind: DetectedMeetingKind;
  appName: string;
  bundleId: string;
}

interface DesktopMeetingDetectionAPI {
  meetingDetection?: { supported?: boolean };
  onMeetingDetected?: (cb: (payload: DetectedMeeting) => void) => () => void;
  setMeetingSelfCapture?: (active: boolean) => void;
}

function readDesktopAPI(): DesktopMeetingDetectionAPI | undefined {
  if (typeof window === "undefined") return undefined;
  return (window as unknown as { desktopAPI?: DesktopMeetingDetectionAPI })
    .desktopAPI;
}

/** True only in the desktop shell, on a platform that can detect meetings. */
export function isMeetingDetectionSupported(): boolean {
  const api = readDesktopAPI();
  return (
    api?.meetingDetection?.supported === true &&
    typeof api.onMeetingDetected === "function"
  );
}

/**
 * Subscribe to "a conferencing app took the microphone". Returns an
 * unsubscribe function — a no-op one on web, so callers can always call it
 * from an effect cleanup.
 */
export function subscribeMeetingDetected(
  callback: (meeting: DetectedMeeting) => void,
): () => void {
  const api = readDesktopAPI();
  if (!api?.onMeetingDetected) return () => {};
  return api.onMeetingDetected(callback);
}

/**
 * Tell the desktop shell whether OUR recorder currently holds the microphone,
 * so ambient detection never prompts about the recording we started.
 */
export function setMeetingSelfCapture(active: boolean): void {
  readDesktopAPI()?.setMeetingSelfCapture?.(active);
}
