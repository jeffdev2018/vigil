/**
 * Honest guidance for active waits and manual retries on the issue execution
 * log. Keeps the wire gate and the product contracts in one place so UI copy
 * cannot invent a different story than claim/rerun behavior.
 */

/**
 * Live hold text for a parked local-directory task. Mirrors the server gate
 * (`waitReasonForStatus`): the DB column is written once on the way into the
 * hold and never cleared, so any status other than waiting must drop it.
 */
export function liveWaitReason(
  status: string | undefined,
  waitReason: string | null | undefined,
): string | undefined {
  if (status !== "waiting_local_directory") return undefined;
  const trimmed = waitReason?.trim();
  return trimmed ? trimmed : undefined;
}

export type ActiveRunGuidanceKind =
  | "waiting_local_directory"
  | "queued"
  | "dispatched";

/**
 * Which cause+action blurb the active execution-log row should show. Running
 * rows need none — the elapsed timer is the signal. Terminal rows use failure
 * labels + the retry honesty tooltip instead.
 */
export function activeRunGuidanceKind(
  status: string | undefined,
): ActiveRunGuidanceKind | null {
  switch (status) {
    case "waiting_local_directory":
    case "queued":
    case "dispatched":
      return status;
    default:
      return null;
  }
}

/**
 * What a manual execution-log Retry (POST rerun) actually keeps. Matches
 * TaskService.RerunIssue / MUL-4869 claim behavior: workdir reuse when still
 * available; session resume only when the source failure did not poison the
 * conversation; external side effects are never rolled back.
 */
export const MANUAL_RETRY_PRESERVES = {
  workDirWhenAvailable: true,
  sessionOnlyIfUnpoisoned: true,
  externalSideEffects: false,
} as const;
