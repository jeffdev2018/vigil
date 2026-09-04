// Microphone-owner readings on platforms without the CoreAudio helper.
//
// Linux: `pactl list source-outputs` lists every stream currently recording
// from a source, with the owning process in its property list.
// Windows: the capability-access consent store records, per app, when it
// last started and stopped using the microphone; a stop time of 0 means the
// app is holding it right now.
//
// Both are pure parsers over command output so they can be unit-tested here;
// `startMicPoller` runs the commands on a timer and feeds the same
// state machine the macOS helper feeds. Untested on real Windows/Linux hosts
// at the time of writing — the formats follow the tools' documented output.

import { execFile } from "node:child_process";
import type { MicMonitorReading, MicOwner } from "./meeting-detection";

/**
 * Binary names as the OS reports them → the process names the matcher knows
 * (see MEETING_APPS / BROWSERS in meeting-detection.ts). Lower-case keys.
 */
const BINARY_ALIASES: Readonly<Record<string, string>> = {
  // Linux
  zoom: "zoom.us",
  "zoom.real": "zoom.us",
  zoomlinux: "zoom.us",
  teams: "MSTeams",
  "teams-for-linux": "MSTeams",
  "ms-teams": "MSTeams",
  slack: "Slack",
  discord: "Discord",
  chrome: "Google Chrome",
  "google-chrome": "Google Chrome",
  "google-chrome-stable": "Google Chrome",
  chromium: "Google Chrome",
  "chromium-browser": "Google Chrome",
  firefox: "Firefox",
  "firefox-bin": "Firefox",
  brave: "Brave Browser",
  "brave-browser": "Brave Browser",
  msedge: "Microsoft Edge",
  "microsoft-edge": "Microsoft Edge",
  // Windows
  "zoom.exe": "zoom.us",
  "ms-teams.exe": "MSTeams",
  "teams.exe": "MSTeams",
  "msteams.exe": "MSTeams",
  "slack.exe": "Slack",
  "discord.exe": "Discord",
  "chrome.exe": "Google Chrome",
  "msedge.exe": "Microsoft Edge",
  "firefox.exe": "Firefox",
  "brave.exe": "Brave Browser",
  "webex.exe": "Webex",
  "atmgr.exe": "Webex",
  "whatsapp.exe": "WhatsApp",
};

/** Map an OS binary name onto a matcher-known process name, else itself. */
export function canonicalBinaryName(name: string): string {
  const key = name.trim().toLowerCase();
  return BINARY_ALIASES[key] ?? name.trim();
}

/**
 * `pactl list source-outputs` prints one block per recording stream:
 *
 *   Source Output #42
 *       Driver: protocol-native.c
 *       ...
 *       Properties:
 *           application.name = "ZOOM VoiceEngine"
 *           application.process.id = "1234"
 *           application.process.binary = "zoom"
 *
 * Monitor streams (recording an output's monitor, e.g. our own loopback
 * capture) are skipped: they do not hold the microphone.
 */
export function parsePactlSourceOutputs(stdout: string): MicOwner[] {
  const owners: MicOwner[] = [];
  const blocks = stdout.split(/^Source Output #\d+/m).slice(1);
  for (const block of blocks) {
    const prop = (key: string): string => {
      const m = block.match(new RegExp(`^\\s*${key.replace(/\./g, "\\.")} = "([^"]*)"`, "m"));
      return m?.[1] ?? "";
    };
    if (/^\s*Source:.*\.monitor\b/m.test(block) || prop("media.class") === "Stream/Input/Audio/Monitor") {
      continue;
    }
    const binary = prop("application.process.binary");
    const name = prop("application.name");
    const pid = Number.parseInt(prop("application.process.id"), 10);
    const label = canonicalBinaryName(binary || name);
    if (!label) continue;
    owners.push({ pid: Number.isFinite(pid) ? pid : 0, bundleId: "", path: label });
  }
  return owners;
}

/**
 * `reg query HKCU\...\CapabilityAccessManager\ConsentStore\microphone /s`
 * prints one key per app that ever used the microphone, followed by its
 * values. Desktop apps sit under `NonPackaged\<path with # for \>`, Store
 * apps directly under `microphone\<PackageFamilyName>`. An app with
 * LastUsedTimeStart set and LastUsedTimeStop == 0 is using the mic now.
 */
export function parseWindowsMicConsent(stdout: string): MicOwner[] {
  const owners: MicOwner[] = [];
  const lines = stdout.split(/\r?\n/);
  let key = "";
  let start = "";
  let stop = "";
  const flush = () => {
    if (!key) return;
    const inUse = start !== "" && /^0x0+$/i.test(stop);
    if (inUse) {
      const last = key.split("\\").pop() ?? key;
      const path = last.includes("#") ? last.replace(/#/g, "\\") : last;
      const base = path.split("\\").pop() ?? path;
      owners.push({ pid: 0, bundleId: "", path: canonicalBinaryName(base) });
    }
    key = "";
    start = "";
    stop = "";
  };
  for (const raw of lines) {
    const line = raw.trimEnd();
    if (/^HKEY_/i.test(line)) {
      flush();
      // The parent "microphone" and "NonPackaged" keys carry values too but
      // no app: they end without a path segment and are dropped by having no
      // LastUsedTimeStart of their own worth trusting.
      key = line.trim();
      continue;
    }
    const m = line.match(/^\s*(LastUsedTimeStart|LastUsedTimeStop)\s+REG_QWORD\s+(\S+)/i);
    if (!m) continue;
    if (m[1]?.toLowerCase() === "lastusedtimestart") start = m[2] ?? "";
    else stop = m[2] ?? "";
  }
  flush();
  // The store-level keys themselves (…\microphone, …\NonPackaged) can carry
  // stale timestamps; only entries that name an app are meaningful.
  return owners.filter((o) => !/^(microphone|NonPackaged)$/i.test(o.path));
}

const WINDOWS_CONSENT_KEY =
  "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\CapabilityAccessManager\\ConsentStore\\microphone";

export interface MicPollerOptions {
  platform: NodeJS.Platform | string;
  intervalMs?: number;
  onReading: (reading: MicMonitorReading) => void;
  onDisabled?: (reason: string) => void;
  /** Test seam: replaces the command runner. */
  run?: (file: string, args: string[]) => Promise<string>;
}

const DEFAULT_INTERVAL_MS = 2_000;
const MAX_CONSECUTIVE_FAILURES = 3;

function runCommand(file: string, args: string[]): Promise<string> {
  return new Promise((resolve, reject) => {
    execFile(file, args, { timeout: 5_000, maxBuffer: 4 << 20, windowsHide: true }, (err, stdout) => {
      if (err) reject(err);
      else resolve(String(stdout));
    });
  });
}

/**
 * Polls the platform's microphone-owner source and reports readings shaped
 * like the macOS helper's. Returns a stop function. Unsupported platforms get
 * a no-op and `onDisabled`.
 */
export function startMicPoller(opts: MicPollerOptions): () => void {
  const run = opts.run ?? runCommand;
  let command: { file: string; args: string[]; parse: (out: string) => MicOwner[] } | null = null;
  if (opts.platform === "linux") {
    command = { file: "pactl", args: ["list", "source-outputs"], parse: parsePactlSourceOutputs };
  } else if (opts.platform === "win32") {
    command = { file: "reg", args: ["query", WINDOWS_CONSENT_KEY, "/s"], parse: parseWindowsMicConsent };
  }
  if (!command) {
    opts.onDisabled?.(`no microphone poller for ${opts.platform}`);
    return () => {};
  }
  const { file, args, parse } = command;
  let failures = 0;
  let stopped = false;
  let inFlight = false;
  const tick = async () => {
    if (stopped || inFlight) return;
    inFlight = true;
    try {
      const owners = parse(await run(file, args));
      failures = 0;
      opts.onReading({ micInUse: owners.length > 0, owners });
    } catch (err) {
      failures += 1;
      if (failures >= MAX_CONSECUTIVE_FAILURES) {
        stopped = true;
        clearInterval(timer);
        opts.onDisabled?.(`${file} failed ${failures} times: ${String(err)}`);
      }
    } finally {
      inFlight = false;
    }
  };
  const timer = setInterval(() => void tick(), opts.intervalMs ?? DEFAULT_INTERVAL_MS);
  void tick();
  return () => {
    stopped = true;
    clearInterval(timer);
  };
}
