import type { AgentTask } from "../types";

/**
 * Normalized run state (F02). Eight wire statuses fold into six states, plus a
 * derived `unresponsive` verdict that is never stored: an active run whose
 * last proof of life is older than the threshold.
 */
export type RunState =
  | "pending"
  | "active"
  | "blocked"
  | "complete"
  | "error"
  | "cancelled"
  | "unresponsive";

export const DEFAULT_RUN_UNRESPONSIVE_AFTER_SECONDS = 90;

/** Pure status → state mapping. Unknown statuses are `pending`, never dropped. */
export function runStateOf(status: string): Exclude<RunState, "unresponsive"> {
  switch (status) {
    case "queued":
    case "deferred":
    case "paused":
      return "pending";
    case "dispatched":
    case "running":
      return "active";
    case "waiting_local_directory":
      return "blocked";
    case "completed":
      return "complete";
    case "failed":
      return "error";
    case "cancelled":
      return "cancelled";
    default:
      return "pending";
  }
}

export function isRunSettled(state: RunState): boolean {
  return state === "complete" || state === "error" || state === "cancelled";
}

type RunLiveness = Pick<AgentTask, "status"> &
  Partial<Pick<AgentTask, "last_activity_at" | "started_at" | "dispatched_at">>;

/**
 * Milliseconds since the run's last known activity, or null when nothing can
 * anchor it (no activity stamp, not started, not dispatched). A client clock
 * behind the server yields a negative age, clamped to zero.
 */
export function runSilenceMs(run: RunLiveness, now: number): number | null {
  const anchor = run.last_activity_at ?? run.started_at ?? run.dispatched_at ?? null;
  if (!anchor) return null;
  const at = new Date(anchor).getTime();
  if (Number.isNaN(at)) return null;
  return Math.max(0, now - at);
}

/**
 * Normalized state with the liveness verdict applied: only an `active` run can
 * be `unresponsive`, pending / blocked / settled runs never are.
 */
export function normalizeRunState(
  run: RunLiveness,
  now: number = Date.now(),
  thresholdSeconds: number = DEFAULT_RUN_UNRESPONSIVE_AFTER_SECONDS,
): RunState {
  const state = runStateOf(run.status);
  if (state !== "active") return state;
  const silence = runSilenceMs(run, now);
  if (silence === null) return state;
  return silence > thresholdSeconds * 1000 ? "unresponsive" : state;
}
