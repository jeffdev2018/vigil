import { app, ipcMain, type BrowserWindow } from "electron";
import { spawn, type ChildProcess } from "node:child_process";
import { existsSync } from "node:fs";
import { join } from "node:path";
import {
  applySelfCapture,
  INITIAL_DETECTOR_STATE,
  parseMicMonitorLine,
  reduceTick,
  type DetectorState,
  type MicOwner,
} from "./meeting-detection";
import { startMicPoller } from "./meeting-detection-pollers";

/**
 * Ambient meeting detection (macOS only).
 *
 * Derived from Rowboat (rowboatlabs/rowboat), Apache-2.0.
 *
 * Spawns the `mic-monitor` Swift helper, which prints one JSON line per
 * change in the set of processes holding the microphone. The pure state
 * machine in `meeting-detection.ts` turns that stream into at most one
 * `meeting:detected` per mic session; the renderer shows a prompt and the
 * user decides whether to record.
 *
 * Entirely best-effort: no helper binary, a non-darwin host, or a helper that
 * keeps dying disables detection with a warning. Nothing here throws.
 */

const POLL_INTERVAL_MS = 1_000;
const HELPER_MAX_RESTARTS = 3;

/**
 * Path to the compiled helper. Resolved from this file's location, like
 * BUNDLED_ICON_PATH in index.ts, so it holds however Electron was started
 * (`electron .`, `electron out/main/index.js`, packaged):
 *  - dev: `out/main` → `../../resources/bin/mic-monitor`, staged by
 *    `scripts/bundle-mic-monitor.mjs` before dev starts.
 *  - packaged: `asarUnpack: resources/**` extracts it next to `app.asar`,
 *    while `__dirname` resolves into `app.asar/`, hence the replace.
 */
export function micMonitorPath(): string {
  return join(__dirname, "..", "..", "resources", "bin", "mic-monitor").replace(
    "app.asar",
    "app.asar.unpacked",
  );
}

let started = false;
let state: DetectorState = INITIAL_DETECTOR_STATE;
let micInUse = false;
let micOwners: MicOwner[] = [];
let selfCapture = false;
let helperRestarts = 0;
let helper: ChildProcess | null = null;
let stopping = false;
let detectionEnabled = true;
let timer: ReturnType<typeof setInterval> | null = null;
let stopPoller: (() => void) | null = null;

function readHelperStdout(child: ChildProcess): void {
  let buffer = "";
  child.stdout?.setEncoding("utf8");
  child.stdout?.on("data", (chunk: string) => {
    buffer += chunk;
    let idx: number;
    while ((idx = buffer.indexOf("\n")) >= 0) {
      const line = buffer.slice(0, idx).trim();
      buffer = buffer.slice(idx + 1);
      if (!line) continue;
      const reading = parseMicMonitorLine(line);
      if (!reading) continue;
      micInUse = reading.micInUse;
      micOwners = reading.owners;
    }
  });
}

function spawnHelper(helperPath: string): void {
  let child: ChildProcess;
  try {
    child = spawn(helperPath, [], { stdio: ["pipe", "pipe", "ignore"] });
  } catch (err) {
    console.error("[meeting-detect] failed to spawn mic-monitor:", err);
    return;
  }
  helper = child;
  readHelperStdout(child);

  child.on("error", (err) => {
    console.error("[meeting-detect] mic-monitor error:", err);
  });
  child.on("exit", (code) => {
    helper = null;
    micInUse = false;
    micOwners = [];
    if (stopping) return;
    if (helperRestarts >= HELPER_MAX_RESTARTS) {
      console.error(
        "[meeting-detect] mic-monitor kept exiting — ambient detection disabled",
      );
      return;
    }
    helperRestarts += 1;
    const delay = 5_000 * helperRestarts;
    console.warn(
      `[meeting-detect] mic-monitor exited (code ${code}); restart ${helperRestarts}/${HELPER_MAX_RESTARTS} in ${delay / 1000}s`,
    );
    setTimeout(() => spawnHelper(helperPath), delay);
  });
}

/**
 * Start watching the microphone. No-op where there is nothing to watch with
 * (no helper binary, an unsupported platform) and where it is already running.
 */
function startDetection(getWindow: () => BrowserWindow | null): void {
  if (timer) return;
  stopping = false;
  helperRestarts = 0;
  state = INITIAL_DETECTOR_STATE;

  if (process.platform === "darwin") {
    const helperPath = micMonitorPath();
    if (!existsSync(helperPath)) {
      console.warn(
        `[meeting-detect] mic-monitor not found at ${helperPath} — ambient detection disabled`,
      );
      return;
    }
    console.log("[meeting-detect] starting ambient meeting detection (CoreAudio helper)");
    spawnHelper(helperPath);
  } else if (process.platform === "linux" || process.platform === "win32") {
    // No native helper here: poll the OS's own view of who records from the
    // microphone (PulseAudio/PipeWire stream list, Windows consent store).
    console.log(`[meeting-detect] starting ambient meeting detection (${process.platform} poller)`);
    stopPoller = startMicPoller({
      platform: process.platform,
      onReading: (reading) => {
        micInUse = reading.micInUse;
        micOwners = reading.owners;
      },
      onDisabled: (reason) => {
        micInUse = false;
        micOwners = [];
        console.warn(`[meeting-detect] ${reason} — ambient detection disabled`);
      },
    });
  } else {
    return;
  }

  timer = setInterval(() => {
    try {
      const result = reduceTick(state, {
        now: Date.now(),
        micInUse,
        owners: micOwners,
        selfCapture,
      });
      state = result.state;
      if (!result.detected) return;
      const window = getWindow();
      if (!window || window.isDestroyed()) return;
      console.log(
        `[meeting-detect] ${result.detected.kind} detected (${result.detected.appName})`,
      );
      window.webContents.send("meeting:detected", result.detected);
    } catch (err) {
      console.error("[meeting-detect] tick failed:", err);
    }
  }, POLL_INTERVAL_MS);
}

/**
 * Stop watching: kill the helper or poller and release the tick. `stopping`
 * also tells the helper's exit handler not to restart it.
 */
function stopDetection(): void {
  stopping = true;
  if (timer) {
    clearInterval(timer);
    timer = null;
  }
  stopPoller?.();
  stopPoller = null;
  helper?.kill();
  helper = null;
  micInUse = false;
  micOwners = [];
}

/**
 * Wire ambient meeting detection to the main window. Safe to call once at
 * startup on every platform; everything but the two IPC channels is a no-op
 * outside macOS/Linux/Windows or without a working backend.
 */
export function setupMeetingDetector(
  getWindow: () => BrowserWindow | null,
): void {
  if (started) return;
  started = true;

  // The renderer reports its own capture so we never prompt about our audio.
  // Registered on every platform: the preload exposes the call unconditionally.
  ipcMain.on("meeting:self-capture", (_event, active: unknown) => {
    selfCapture = active === true;
    state = applySelfCapture(state, selfCapture);
  });

  // Settings → Preferences. Turning it off stops the watcher outright rather
  // than muting its prompts: the point of the preference is not running a
  // microphone watcher at all.
  ipcMain.on("meeting:detection-enabled", (_event, enabled: unknown) => {
    const next = enabled !== false;
    if (next === detectionEnabled) return;
    detectionEnabled = next;
    if (next) startDetection(getWindow);
    else stopDetection();
  });

  if (detectionEnabled) startDetection(getWindow);

  app.on("will-quit", () => {
    stopDetection();
  });
}
