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
 * Path to the compiled helper, resolved the same way `bundledCliPath()` in
 * daemon-manager.ts resolves the Go CLI:
 *  - dev: `app.getAppPath()` → `apps/desktop`, i.e. `resources/bin/mic-monitor`
 *    (staged by `scripts/bundle-mic-monitor.mjs` before dev starts).
 *  - packaged: `asarUnpack: resources/**` extracts it next to `app.asar`, so
 *    swap the segment to get an executable filesystem path.
 */
export function micMonitorPath(): string {
  return join(app.getAppPath(), "resources", "bin", "mic-monitor").replace(
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
 * Wire ambient meeting detection to the main window. Safe to call once at
 * startup on every platform; everything but the self-capture IPC is a no-op
 * outside macOS or without the helper binary.
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

  if (process.platform !== "darwin") return;

  const helperPath = micMonitorPath();
  if (!existsSync(helperPath)) {
    console.warn(
      `[meeting-detect] mic-monitor not found at ${helperPath} — ambient detection disabled`,
    );
    return;
  }

  console.log("[meeting-detect] starting ambient meeting detection");
  spawnHelper(helperPath);

  const timer = setInterval(() => {
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

  app.on("will-quit", () => {
    clearInterval(timer);
    helper?.kill();
  });
}
