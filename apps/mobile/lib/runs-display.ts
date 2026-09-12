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
import type { AgentTask } from "@multica/core/types";
import type { Run, RunBlocker, RunsSummary } from "@/data/schemas";
import { formatPostmortemCost } from "./postmortem-display";
import { stripMarkdown } from "./strip-markdown";

export const formatRunCost = formatPostmortemCost;

/**
 * Mirrors packages/core/runs/fleet-schemas.ts `runCostKnown`: the server now
 * prices usage like budget settlement and sends `cost_known`; an older one
 * sums only provider-reported ticks, where only a positive amount is a figure.
 */
export function runCostKnown(run: { cost_known?: boolean; cost_usd_ticks: number }): boolean {
  return run.cost_known === true || (run.cost_known === undefined && run.cost_usd_ticks > 0);
}

/** "Cost today", naming the started runs the figure cannot price. */
export function costTodayLabel(summary: Pick<RunsSummary, "cost_since_usd_ticks" | "cost_unknown_since">): string {
  const cost = formatRunCost(summary.cost_since_usd_ticks);
  return summary.cost_unknown_since > 0 ? `${cost} + ${summary.cost_unknown_since} unknown` : cost;
}

/** A run row's summary line: the trigger text with mention markdown rendered
 *  as its label, else a kind-based fallback. */
export function runSummaryText(task: Pick<AgentTask, "kind" | "trigger_summary">): string {
  const summary = stripMarkdown(task.trigger_summary ?? "").replace(/\s+/g, " ").trim();
  if (summary) return summary;
  switch (task.kind) {
    case "comment":
      return "Comment task";
    case "autopilot":
      return "Autopilot run";
    case "chat":
      return "Chat task";
    case "quick_create":
      return "Quick create";
    default:
      return "Task";
  }
}

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

// Runs whose event log the replay screen can show.
const REPLAYABLE_STATUSES: ReadonlySet<Run["status"]> = new Set(["running", "completed", "failed", "cancelled"]);

/**
 * What tapping a run row opens. A run with an event log opens its replay. On
 * the fleet screen a run with nothing to replay yet (queued, deferred,
 * dispatched, paused) opens its issue — the web Runs page links every row to
 * its issue (packages/views/runs/components/runs-page.tsx). Inside an issue's
 * own runs sheet that would reopen the issue the user is on, so it stays inert.
 */
export function runRowTapTarget(status: Run["status"], opts: { inFleet: boolean }): "replay" | "issue" | null {
  if (REPLAYABLE_STATUSES.has(status)) return "replay";
  return opts.inFleet ? "issue" : null;
}
