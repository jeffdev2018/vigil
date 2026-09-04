import { basename } from "node:path";

/**
 * Pure decision layer for ambient meeting detection. No Electron, no timers,
 * no child processes — the Electron wiring lives in `meeting-detector.ts` and
 * the matrix of rules below is tested directly in `meeting-detector.test.ts`.
 *
 * Derived from Rowboat (rowboatlabs/rowboat), Apache-2.0.
 *
 * The signal is one JSON line per state change from the `mic-monitor` helper:
 * which processes currently hold the microphone, with their bundle IDs. When
 * a known conferencing app has held the mic continuously for a few seconds —
 * and it is not our own capture — we report one detection for that mic
 * session. Never auto-records: the renderer shows a prompt and waits.
 */

/** Mic in use continuously this long before prompting. Filters out Siri,
 *  dictation bursts, and app mic-permission probes. */
export const MIC_DEBOUNCE_MS = 5_000;
/** Mic idle this long ends the session and re-arms the prompt. */
export const MIC_SESSION_RESET_MS = 30_000;

export type DetectedMeetingKind = "huddle" | "call" | "meeting";

export interface MicOwner {
  pid: number;
  bundleId: string;
  path: string;
}

export interface DetectedMeeting {
  kind: DetectedMeetingKind;
  /** Human label of the app that owns the call, e.g. "Slack", "Zoom". */
  appName: string;
  /** Bundle ID of the owning process, "" when the helper could not read one. */
  bundleId: string;
}

interface PlatformMatch {
  app: string;
  kind: DetectedMeetingKind;
}

// Bundle-ID prefixes (lowercased) → app. The primary matcher: exact and
// stable, and it covers helper processes too (com.google.Chrome.helper …).
const BUNDLE_MATCHERS: ReadonlyArray<{
  prefix: string;
  app: string;
  kind: DetectedMeetingKind;
}> = [
  { prefix: "us.zoom", app: "Zoom", kind: "meeting" },
  { prefix: "com.microsoft.teams", app: "Microsoft Teams", kind: "meeting" },
  { prefix: "com.cisco.webex", app: "Webex", kind: "meeting" },
  { prefix: "com.webex", app: "Webex", kind: "meeting" },
  { prefix: "com.tinyspeck.slackmacgap", app: "Slack", kind: "huddle" },
  { prefix: "com.apple.facetime", app: "FaceTime", kind: "call" },
  { prefix: "net.whatsapp", app: "WhatsApp", kind: "call" },
  { prefix: "com.hnc.discord", app: "Discord", kind: "call" },
  // Browsers — generic "meeting".
  { prefix: "com.google.chrome", app: "Google Chrome", kind: "meeting" },
  { prefix: "com.apple.safari", app: "Safari", kind: "meeting" },
  { prefix: "com.apple.webkit", app: "Safari", kind: "meeting" },
  { prefix: "org.mozilla.firefox", app: "Firefox", kind: "meeting" },
  { prefix: "com.brave.browser", app: "Brave Browser", kind: "meeting" },
  { prefix: "com.microsoft.edgemac", app: "Microsoft Edge", kind: "meeting" },
  { prefix: "company.thebrowser", app: "Arc", kind: "meeting" },
];

// Multica's own capture (electron-builder.yml `appId`) and the dev Electron
// shell, which runs under Electron's bundle id — never a "meeting".
const SELF_BUNDLE_PREFIXES = ["ai.multica", "com.github.electron"];

const BROWSER_APPS: ReadonlySet<string> = new Set([
  "Google Chrome",
  "Safari",
  "Firefox",
  "Brave Browser",
  "Microsoft Edge",
  "Arc",
]);

// Process-name fallbacks (case-insensitive prefix on the executable
// basename), used when an owner has no bundle ID.
const MEETING_APPS: ReadonlyArray<{
  proc: string;
  app: string;
  kind: DetectedMeetingKind;
}> = [
  { proc: "zoom.us", app: "Zoom", kind: "meeting" },
  { proc: "MSTeams", app: "Microsoft Teams", kind: "meeting" },
  { proc: "Microsoft Teams", app: "Microsoft Teams", kind: "meeting" },
  { proc: "Webex", app: "Webex", kind: "meeting" },
  { proc: "FaceTime", app: "FaceTime", kind: "call" },
  { proc: "WhatsApp", app: "WhatsApp", kind: "call" },
  { proc: "Slack", app: "Slack", kind: "huddle" },
  { proc: "Discord", app: "Discord", kind: "call" },
];

const BROWSERS = [
  "Google Chrome",
  "Safari",
  "Arc",
  "Brave Browser",
  "Microsoft Edge",
  "Firefox",
  "Dia",
  "Comet",
];

/** Match one process name to an app, by case-insensitive prefix. */
function matchProcessName(name: string): PlatformMatch | null {
  const lower = name.toLowerCase();
  // Safari captures through WebKit's out-of-process media stack.
  if (lower.startsWith("com.apple.webkit")) {
    return { app: "Safari", kind: "meeting" };
  }
  // Firefox media capture lives in plugin-container.
  if (lower.startsWith("plugin-container")) {
    return { app: "Firefox", kind: "meeting" };
  }
  for (const candidate of MEETING_APPS) {
    if (lower.startsWith(candidate.proc.toLowerCase())) {
      return { app: candidate.app, kind: candidate.kind };
    }
  }
  for (const browser of BROWSERS) {
    if (lower.startsWith(browser.toLowerCase())) {
      return { app: browser, kind: "meeting" };
    }
  }
  return null;
}

/** Match a mic owner to an app, to Multica itself, or to nothing. */
export function matchOwner(owner: MicOwner): PlatformMatch | "self" | null {
  const bundle = owner.bundleId.toLowerCase();
  if (bundle) {
    for (const prefix of SELF_BUNDLE_PREFIXES) {
      if (bundle.startsWith(prefix)) return "self";
    }
    for (const matcher of BUNDLE_MATCHERS) {
      if (bundle.startsWith(matcher.prefix)) {
        return { app: matcher.app, kind: matcher.kind };
      }
    }
  }
  const name = owner.path ? basename(owner.path) : "";
  return name ? matchProcessName(name) : null;
}

/**
 * The owner most likely to be the call. Dedicated conferencing apps win over
 * browsers when several hold the mic at once. Owners known but none
 * call-capable (voice memo, dictation, a screen recorder) → null, stay quiet.
 */
export function pickMeetingOwner(owners: MicOwner[]): DetectedMeeting | null {
  const matched: Array<{ owner: MicOwner; match: PlatformMatch }> = [];
  for (const owner of owners) {
    const match = matchOwner(owner);
    if (match === null || match === "self") continue;
    matched.push({ owner, match });
  }
  const best =
    matched.find((m) => !BROWSER_APPS.has(m.match.app)) ?? matched[0] ?? null;
  if (!best) return null;
  return {
    kind: best.match.kind,
    appName: best.match.app,
    bundleId: best.owner.bundleId,
  };
}

export interface MicMonitorReading {
  micInUse: boolean;
  owners: MicOwner[];
}

/** Parse one stdout line from the helper. Returns null for anything malformed. */
export function parseMicMonitorLine(line: string): MicMonitorReading | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(line);
  } catch {
    return null;
  }
  if (typeof parsed !== "object" || parsed === null) return null;
  const record = parsed as { micInUse?: unknown; owners?: unknown };
  if (typeof record.micInUse !== "boolean") return null;
  const owners = Array.isArray(record.owners)
    ? record.owners.filter(
        (o: unknown): o is MicOwner =>
          typeof (o as MicOwner)?.pid === "number" &&
          typeof (o as MicOwner)?.bundleId === "string" &&
          typeof (o as MicOwner)?.path === "string",
      )
    : [];
  return { micInUse: record.micInUse, owners };
}

export interface DetectorState {
  micInUseSince: number | null;
  micIdleSince: number | null;
  /** True once this mic session has been reported (or claimed as our own). */
  sessionNotified: boolean;
}

export const INITIAL_DETECTOR_STATE: DetectorState = {
  micInUseSince: null,
  micIdleSince: null,
  sessionNotified: false,
};

export interface DetectorTick {
  now: number;
  micInUse: boolean;
  owners: MicOwner[];
  /** True while our own renderer holds the mic (it reports this over IPC). */
  selfCapture: boolean;
}

/**
 * One tick of the detection state machine. Fires at most once per continuous
 * mic-in-use session; the session re-arms after MIC_SESSION_RESET_MS idle.
 */
export function reduceTick(
  state: DetectorState,
  input: DetectorTick,
): { state: DetectorState; detected: DetectedMeeting | null } {
  const { now, micInUse, owners, selfCapture } = input;

  if (!micInUse) {
    const micIdleSince = state.micIdleSince ?? now;
    return {
      state: {
        micInUseSince: null,
        micIdleSince,
        sessionNotified:
          state.sessionNotified && now - micIdleSince < MIC_SESSION_RESET_MS,
      },
      detected: null,
    };
  }

  const micInUseSince = state.micInUseSince ?? now;
  const held: DetectorState = { ...state, micInUseSince, micIdleSince: null };

  if (state.sessionNotified) return { state: held, detected: null };
  if (selfCapture) {
    // The session is ours: mark it handled so it does not prompt the moment
    // our capture stops while the conferencing app keeps the mic.
    return { state: { ...held, sessionNotified: true }, detected: null };
  }
  if (now - micInUseSince < MIC_DEBOUNCE_MS) {
    return { state: held, detected: null };
  }

  const detected = pickMeetingOwner(owners);
  // Nothing call-capable owns the mic yet — stay quiet, but leave the session
  // open so a conferencing app joining later still prompts.
  if (!detected) return { state: held, detected: null };
  return { state: { ...held, sessionNotified: true }, detected };
}

/**
 * Our own capture also flips the mic-in-use signal. Starting one closes the
 * current session so we never prompt about our own audio.
 */
export function applySelfCapture(
  state: DetectorState,
  active: boolean,
): DetectorState {
  return active ? { ...state, sessionNotified: true } : state;
}
