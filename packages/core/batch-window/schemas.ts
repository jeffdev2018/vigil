import { z } from "zod";

// Off-peak batch lane (K45). A workspace declares one window during which
// autopilots marked `batch_eligible` are dispatched into the batch lane: their
// runs are claimed only after every synchronous task of the same agent and
// runtime. The trade the user accepts is "cheaper, arrives later".
//
// The window is wall-clock, read in its own IANA timezone. `start_local_time`
// is inclusive, `end_local_time` is exclusive, and an end before the start
// crosses midnight (22:00 → 06:00) — which is the shape most workspaces want.

/** Which lane a task was dispatched into. Mirrors service/batch_window.go. */
export type DispatchLane = "sync" | "batch";

/** The workspace's off-peak window. Disabled means no batch lane at all. */
export interface BatchWindow {
  enabled: boolean;
  /** Inclusive local start, "HH:MM". */
  start_local_time: string;
  /** Exclusive local end, "HH:MM". */
  end_local_time: string;
  /** IANA timezone the two times are read in. */
  timezone: string;
}

// Every field degrades rather than throws: a drifted response must read as
// "no off-peak window" — never as one covering all of time, which would look
// to the user like every autopilot silently started waiting. `.loose()` keeps
// fields a newer server added.
export const BatchWindowSchema = z
  .object({
    enabled: z.boolean().catch(false),
    start_local_time: z.string().catch(""),
    end_local_time: z.string().catch(""),
    timezone: z.string().catch("UTC"),
  })
  .loose();

export const BATCH_WINDOW_DEFAULTS: BatchWindow = {
  enabled: false,
  start_local_time: "",
  end_local_time: "",
  timezone: "UTC",
};

/**
 * Whether the workspace has a window an autopilot could actually be deferred
 * into. Drives the disabled state of the per-autopilot toggle: opting one
 * autopilot in while no window exists would be a promise nothing keeps.
 *
 * Deliberately more than `enabled === true`: the server refuses to STORE an
 * enabled window with unusable times, but a response can still drift, and the
 * toggle must not offer a lane the scheduler would never apply.
 */
export function hasUsableBatchWindow(window: BatchWindow | undefined): boolean {
  if (!window || window.enabled !== true) return false;
  const start = parseWallClock(window.start_local_time);
  const end = parseWallClock(window.end_local_time);
  return start !== null && end !== null && start !== end;
}

/**
 * The same validation the server applies, run client-side so the form says
 * what is wrong before a round trip. Returns a reason key the caller
 * localizes, or null when the window is storable.
 */
export function batchWindowProblem(
  window: BatchWindow,
): "invalid_time" | "equal_bounds" | null {
  if (window.enabled !== true) return null;
  const start = parseWallClock(window.start_local_time);
  const end = parseWallClock(window.end_local_time);
  if (start === null || end === null) return "invalid_time";
  if (start === end) return "equal_bounds";
  return null;
}

/**
 * "HH:MM" to minutes since local midnight, or null. Strict on purpose: the
 * stored form is exactly what an `<input type="time">` produces, so anything
 * else came from a drifted response rather than from this form.
 */
function parseWallClock(value: string): number | null {
  const match = /^(\d{2}):(\d{2})$/.exec(value ?? "");
  if (!match) return null;
  const hour = Number(match[1]);
  const minute = Number(match[2]);
  if (hour > 23 || minute > 59) return null;
  return hour * 60 + minute;
}
