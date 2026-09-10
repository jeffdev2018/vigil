/**
 * Pure display helpers for the Runs fleet screen (OS plan, chantier 4).
 *
 * Section grouping and the "silent" verdict reuse
 * `@multica/core/agents/run-state.ts` directly — it's a pure
 * normalized-state module (only imports `AgentTask` as a type; no
 * react-dom, localStorage, process.env, or `../api` coupling), so it is on
 * the "types and pure functions from @multica/core" whitelist
 * (apps/mobile/CLAUDE.md), unlike the approvals feed's schema/queries
 * chain which pulls in web's live `api` singleton.
 *
 * Duration/silence formatting mirrors `packages/views/dashboard/utils.ts`
 * `formatDuration` (seconds-based, "two segments max — three segments adds
 * visual noise without precision the dashboard actually needs"). Not
 * imported: `packages/views` is web/desktop only. Copied and adapted to a
 * ms input instead. Cost formatting reuses `lib/postmortem-display.ts`'s
 * `formatPostmortemCost` verbatim — same 1e-10-USD-tick formula, no reason
 * for a second copy in the same app.
 */
import {
  DEFAULT_RUN_UNRESPONSIVE_AFTER_SECONDS,
  isRunSettled,
  runStateOf,
} from "@multica/core/agents/run-state";
import type { Run, RunBlocker } from "@/data/schemas";
import { formatPostmortemCost } from "./postmortem-display";

export const formatRunCost = formatPostmortemCost;

export type RunSection = "queued" | "running" | "blocked" | "finished";

type SectionInput = Pick<Run, "status" | "blocked_on">;

/**
 * Which of the fleet's four buckets a run belongs in. `blocked_on` wins
 * over the raw status: a "running" task the server has flagged as waiting
 * on a gate / decision / goal question / transition still belongs in the
 * blocked bucket, not the running one — mirrors how the summary counts
 * treat "blocked" as a property layered on top of the active runs
 * (`runsSummary` in server/internal/handler/runs.go), not a status of its
 * own.
 */
export function runSection(run: SectionInput): RunSection {
  if (run.blocked_on) return "blocked";
  const state = runStateOf(run.status);
  if (isRunSettled(state)) return "finished";
  if (state === "pending") return "queued";
  // "blocked" here is runStateOf's own waiting_local_directory case — the
  // server's blocked_on for that status is usually already set (see
  // runBlockers in server/internal/handler/runs.go), so the check above
  // is the common path; this covers the same status with no blocker
  // resolved yet (e.g. a stale read mid-write).
  if (state === "blocked") return "blocked";
  return "running";
}

/** Groups runs into the four buckets, in a stable {queued, running,
 *  blocked, finished} shape. Empty buckets stay present as `[]` so a
 *  caller can decide to hide them rather than branch on `undefined`. */
export function groupRunsBySection<T extends SectionInput>(
  runs: readonly T[],
): Record<RunSection, T[]> {
  const out: Record<RunSection, T[]> = {
    queued: [],
    running: [],
    blocked: [],
    finished: [],
  };
  for (const run of runs) out[runSection(run)].push(run);
  return out;
}

/** A running run whose last reported activity is older than the fleet's
 *  unresponsive threshold. Queued and terminal runs are never "silent" —
 *  only an active run can go quiet. */
export function isRunSilent(run: Pick<Run, "status" | "silence_ms">): boolean {
  return (
    run.status === "running" &&
    run.silence_ms > DEFAULT_RUN_UNRESPONSIVE_AFTER_SECONDS * 1000
  );
}

const BLOCKER_KIND_LABEL: Record<string, string> = {
  gate: "Approval",
  decision: "Decision",
  goal_question: "Question",
  transition: "Status change",
  local_directory: "Local directory",
  paused: "Paused",
  deferred: "Deferred",
};

/**
 * Short chip label for a run's blocker: kind + the server's own summary
 * text (e.g. "Approval: git push"). The server already renders a
 * human-readable `summary` for every kind (gate type, the goal question's
 * prompt, "from → to", the wait reason…), so this only adds the kind
 * prefix. Falls back to the raw kind for a kind this build doesn't
 * recognise yet, and to a generic label if even the kind is missing —
 * enum drift downgrades, it never disappears.
 */
export function blockerLabel(blocker: RunBlocker | null | undefined): string | null {
  if (!blocker) return null;
  const kind = BLOCKER_KIND_LABEL[blocker.kind] ?? (blocker.kind || "Blocked");
  const summary = blocker.summary?.trim();
  return summary ? `${kind}: ${summary}` : kind;
}

function formatFromSeconds(seconds: number, zeroLabel: string): string {
  if (seconds < 0 || !Number.isFinite(seconds)) return zeroLabel;
  if (seconds < 60) {
    if (seconds < 1) return zeroLabel;
    return `${Math.round(seconds)}s`;
  }
  const totalMinutes = Math.floor(seconds / 60);
  const hours = Math.floor(totalMinutes / 60);
  const mins = totalMinutes % 60;
  if (hours === 0) {
    const secs = Math.floor(seconds) % 60;
    return secs > 0 ? `${mins}m ${secs}s` : `${mins}m`;
  }
  if (hours >= 24) {
    const days = Math.floor(hours / 24);
    const h = hours % 24;
    return h > 0 ? `${days}d ${h}h` : `${days}d`;
  }
  return mins > 0 ? `${hours}h ${mins}m` : `${hours}h`;
}

/** Run duration, ms → "2m 14s" / "1h 05m" / "2d 3h". Zero, negative or NaN
 *  input (a run with no `started_at` yet) renders "0s". */
export function formatRunDuration(ms: number): string {
  return formatFromSeconds(ms / 1000, "0s");
}

/** Same formatting as `formatRunDuration`, named separately so call sites
 *  read clearly (`formatRunSilence(run.silence_ms)` next to a "Silent"
 *  chip vs. the row's own duration). */
export function formatRunSilence(ms: number): string {
  return formatFromSeconds(ms / 1000, "0s");
}
