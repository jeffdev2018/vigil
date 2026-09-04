import { desktopCapturer } from "electron";
import { execFile } from "node:child_process";
import { promisify } from "node:util";

/**
 * Audio/video capture wiring for the renderer's meeting recorder.
 *
 * The recorder calls `getUserMedia({ audio })` for the microphone and
 * `getDisplayMedia({ audio, video })` for system audio (it drops the video
 * track immediately — Chromium refuses audio-only display capture). Without
 * a display-media handler Electron denies the second call outright, so a
 * desktop meeting would only ever capture the local mic.
 */

/**
 * Which shape of display-media answer this request gets.
 *
 * `frame-loopback` answers with the requesting frame as the mandatory video
 * source. On Linux, enumerating screens goes through the Wayland screencast
 * portal, which can block on a system dialog or hang outright — and an
 * audio-only consumer does not need a real screen anyway.
 *
 * `screen-loopback` enumerates screens and answers with the first one.
 */
export type DisplayMediaPlan = "frame-loopback" | "screen-loopback";

export function planDisplayMedia(
  platform: NodeJS.Platform | string,
  audioRequested: boolean,
  hasFrame: boolean,
): DisplayMediaPlan {
  if (platform === "linux" && audioRequested && hasFrame) {
    return "frame-loopback";
  }
  return "screen-loopback";
}

/**
 * On Linux, Chromium's loopback capture records the default sink's monitor
 * through the PulseAudio layer, at the monitor source's own volume. Desktop
 * tools sometimes leave that volume near zero — invisible plumbing that does
 * not affect what the user hears — which turns the whole capture into digital
 * silence with no error anywhere. Raise it back to 100% before capture starts
 * (raise only, so a deliberate >100% boost is left alone). Best-effort: no
 * pactl or no PulseAudio layer just skips.
 */
async function ensureLinuxMonitorVolume(): Promise<void> {
  const execFileP = promisify(execFile);
  try {
    const { stdout: sinkOut } = await execFileP("pactl", ["get-default-sink"], {
      timeout: 3_000,
    });
    const monitor = `${sinkOut.trim()}.monitor`;
    const { stdout: volOut } = await execFileP(
      "pactl",
      ["get-source-volume", monitor],
      { timeout: 3_000 },
    );
    const percents = [...volOut.matchAll(/(\d+)%/g)].map((m) => Number(m[1]));
    if (percents.length === 0 || Math.min(...percents) >= 100) return;
    await execFileP("pactl", ["set-source-volume", monitor, "100%"], {
      timeout: 3_000,
    });
    console.log(
      `[media] raised ${monitor} volume from ${Math.min(...percents)}% to 100% for system-audio capture`,
    );
  } catch {
    // pactl missing or a non-Pulse audio stack — nothing to fix here.
  }
}

// createWindow() can run again on macOS ("activate" after every window was
// closed) and the session is shared, so guard against re-registering.
const configuredSessions = new WeakSet<Electron.Session>();

/**
 * Auto-approve display-media requests and route system audio as loopback.
 * No permission handler is installed on purpose: Electron grants `media`
 * (microphone) by default, and adding a deny-by-default handler would change
 * behaviour for every other permission the app already relies on.
 */
export function configureMediaCapture(session: Electron.Session): void {
  if (configuredSessions.has(session)) return;
  configuredSessions.add(session);

  session.setDisplayMediaRequestHandler(async (request, callback) => {
    try {
      const plan = planDisplayMedia(
        process.platform,
        request.audioRequested,
        request.frame != null,
      );
      if (plan === "frame-loopback" && request.frame) {
        await ensureLinuxMonitorVolume();
        callback({ video: request.frame, audio: "loopback" });
        return;
      }
      const sources = await desktopCapturer.getSources({ types: ["screen"] });
      if (sources.length === 0) {
        // No screen to hand back: the renderer treats a rejected display
        // capture as "microphone only" and keeps recording.
        callback({});
        return;
      }
      callback({ video: sources[0], audio: "loopback" });
    } catch (err) {
      console.error("[media] display-media request failed:", err);
      callback({});
    }
  });
}
