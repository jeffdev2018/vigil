// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  applySelfCapture,
  INITIAL_DETECTOR_STATE,
  MIC_DEBOUNCE_MS,
  MIC_SESSION_RESET_MS,
  matchOwner,
  parseMicMonitorLine,
  reduceTick,
  type DetectorState,
  type MicOwner,
} from "./meeting-detection";

function owner(bundleId: string, path = "", pid = 100): MicOwner {
  return { pid, bundleId, path };
}

const ZOOM = owner("us.zoom.xos", "/Applications/zoom.us.app/Contents/MacOS/zoom.us");
const CHROME = owner("com.google.Chrome.helper", "", 200);
const SELF = owner("ai.multica.desktop", "", 300);

/** Run `ticks` ticks one second apart, starting at `t0`. */
function run(
  ticks: Array<{ micInUse: boolean; owners?: MicOwner[]; selfCapture?: boolean }>,
  t0 = 1_000_000,
  step = 1_000,
) {
  let state: DetectorState = INITIAL_DETECTOR_STATE;
  const detections: string[] = [];
  ticks.forEach((tick, i) => {
    const result = reduceTick(state, {
      now: t0 + i * step,
      micInUse: tick.micInUse,
      owners: tick.owners ?? [],
      selfCapture: tick.selfCapture ?? false,
    });
    state = result.state;
    if (result.detected) detections.push(result.detected.appName);
  });
  return { state, detections };
}

describe("matchOwner", () => {
  it("matches a conferencing app by bundle-id prefix", () => {
    expect(matchOwner(ZOOM)).toEqual({ app: "Zoom", kind: "meeting" });
    expect(matchOwner(owner("com.tinyspeck.slackmacgap"))).toEqual({
      app: "Slack",
      kind: "huddle",
    });
    expect(matchOwner(owner("com.apple.FaceTime"))).toEqual({
      app: "FaceTime",
      kind: "call",
    });
    // Helper processes carry the parent's prefix.
    expect(matchOwner(CHROME)).toEqual({ app: "Google Chrome", kind: "meeting" });
  });

  it("excludes our own capture and the dev Electron shell", () => {
    expect(matchOwner(SELF)).toBe("self");
    expect(matchOwner(owner("com.github.Electron"))).toBe("self");
  });

  it("falls back to the executable basename when there is no bundle id", () => {
    expect(matchOwner(owner("", "/Applications/Slack.app/Contents/MacOS/Slack"))).toEqual({
      app: "Slack",
      kind: "huddle",
    });
    expect(matchOwner(owner("", "/usr/bin/plugin-container"))).toEqual({
      app: "Firefox",
      kind: "meeting",
    });
  });

  it("returns null for a mic owner that is not call-capable", () => {
    expect(matchOwner(owner("com.apple.VoiceMemos"))).toBeNull();
    expect(matchOwner(owner("", ""))).toBeNull();
  });
});

describe("parseMicMonitorLine", () => {
  it("parses a helper line and drops malformed owners", () => {
    const parsed = parseMicMonitorLine(
      '{"micInUse":true,"owners":[{"pid":1,"bundleId":"us.zoom.xos","path":"/z"},{"pid":"x"}]}',
    );
    expect(parsed).toEqual({
      micInUse: true,
      owners: [{ pid: 1, bundleId: "us.zoom.xos", path: "/z" }],
    });
  });

  it("returns null for non-JSON or a missing micInUse flag", () => {
    expect(parseMicMonitorLine("not json")).toBeNull();
    expect(parseMicMonitorLine('{"owners":[]}')).toBeNull();
  });
});

describe("reduceTick", () => {
  it("waits out the debounce before prompting", () => {
    const seconds = MIC_DEBOUNCE_MS / 1_000;
    const early = run(
      Array.from({ length: seconds }, () => ({ micInUse: true, owners: [ZOOM] })),
    );
    expect(early.detections).toEqual([]);

    const enough = run(
      Array.from({ length: seconds + 1 }, () => ({ micInUse: true, owners: [ZOOM] })),
    );
    expect(enough.detections).toEqual(["Zoom"]);
  });

  it("prompts once per continuous mic session", () => {
    const { detections } = run(
      Array.from({ length: 20 }, () => ({ micInUse: true, owners: [ZOOM] })),
    );
    expect(detections).toEqual(["Zoom"]);
  });

  it("re-arms after the mic has been idle long enough", () => {
    const idleTicks = MIC_SESSION_RESET_MS / 1_000 + 1;
    const { detections } = run([
      ...Array.from({ length: 10 }, () => ({ micInUse: true, owners: [ZOOM] })),
      ...Array.from({ length: idleTicks }, () => ({ micInUse: false })),
      ...Array.from({ length: 10 }, () => ({ micInUse: true, owners: [ZOOM] })),
    ]);
    expect(detections).toEqual(["Zoom", "Zoom"]);
  });

  it("does not re-arm after a short mic gap", () => {
    const { detections } = run([
      ...Array.from({ length: 10 }, () => ({ micInUse: true, owners: [ZOOM] })),
      ...Array.from({ length: 3 }, () => ({ micInUse: false })),
      ...Array.from({ length: 10 }, () => ({ micInUse: true, owners: [ZOOM] })),
    ]);
    expect(detections).toEqual(["Zoom"]);
  });

  it("never prompts while our own recorder holds the mic", () => {
    const { detections } = run(
      Array.from({ length: 20 }, () => ({
        micInUse: true,
        owners: [SELF, ZOOM],
        selfCapture: true,
      })),
    );
    expect(detections).toEqual([]);
  });

  it("stays quiet when only our own process owns the mic", () => {
    const { detections } = run(
      Array.from({ length: 20 }, () => ({ micInUse: true, owners: [SELF] })),
    );
    expect(detections).toEqual([]);
  });

  it("stays quiet for a non-conferencing mic owner but still prompts when one joins", () => {
    const { detections } = run([
      ...Array.from({ length: 10 }, () => ({
        micInUse: true,
        owners: [owner("com.apple.VoiceMemos")],
      })),
      { micInUse: true, owners: [owner("com.apple.VoiceMemos"), ZOOM] },
    ]);
    expect(detections).toEqual(["Zoom"]);
  });

  it("prefers a dedicated conferencing app over a browser", () => {
    const { detections } = run(
      Array.from({ length: 10 }, () => ({
        micInUse: true,
        owners: [CHROME, ZOOM],
      })),
    );
    expect(detections).toEqual(["Zoom"]);
  });

  it("applySelfCapture closes the current session so stopping does not prompt", () => {
    const armed = applySelfCapture(INITIAL_DETECTOR_STATE, true);
    expect(armed.sessionNotified).toBe(true);
    // Turning it off must not re-open the session on its own.
    expect(applySelfCapture(armed, false)).toEqual(armed);
  });
});
