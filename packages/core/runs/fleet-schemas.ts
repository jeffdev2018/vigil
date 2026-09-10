import { z } from "zod";
import { AgentTaskSchema } from "../api/schemas";
import { RunHaltSchema, EMPTY_RUN_HALT } from "../approvals/schemas";

// Runs fleet page (OS plan, chantier 4). One list of every run in the
// workspace — queued, running, waiting on somebody, over — with what each
// costs and what it is blocked on. Server source of truth:
// server/internal/handler/runs.go (RunResponse, RunsSummary, RunsResponse).

export const RunIssueRefSchema = z
  .object({
    id: z.string().catch(""),
    identifier: z.string().catch(""),
    title: z.string().catch(""),
    status: z.string().catch(""),
  })
  .loose();
export type RunIssueRef = z.infer<typeof RunIssueRefSchema>;

/** Known blocker kinds. Open on the wire — see RunSchema's comment. */
export type RunBlockerKind =
  | "gate"
  | "decision"
  | "goal_question"
  | "transition"
  | "local_directory"
  | "paused"
  | "deferred"
  | (string & {});

export const RunBlockerSchema = z
  .object({
    kind: z.string().catch(""),
    id: z.string().optional().catch(undefined),
    decision_id: z.string().optional().catch(undefined),
    summary: z.string().catch(""),
    since: z.string().nullable().catch(null),
  })
  .loose();
export type RunBlocker = z.infer<typeof RunBlockerSchema>;

// One fleet row: the task as the app already knows it (AgentTaskSchema),
// plus the names, cost and blocker the list needs. `.extend()` keeps every
// independent-degradation rule AgentTaskSchema already applies to usage,
// routing, plan, etc — a malformed fleet-only field must not cost the row
// its task fields, and vice versa.
export const RunSchema = AgentTaskSchema.extend({
  agent_name: z.string().catch(""),
  issue: RunIssueRefSchema.nullable().catch(null),
  cost_usd_ticks: z.number().catch(0),
  duration_ms: z.number().catch(0),
  silence_ms: z.number().catch(0),
  blocked_on: RunBlockerSchema.nullable().catch(null),
});
export type Run = z.infer<typeof RunSchema>;

export const RunsSummarySchema = z
  .object({
    active: z.number().catch(0),
    queued: z.number().catch(0),
    running: z.number().catch(0),
    blocked: z.number().catch(0),
    completed_since: z.number().catch(0),
    failed_since: z.number().catch(0),
    cancelled_since: z.number().catch(0),
    cost_since_usd_ticks: z.number().catch(0),
    since: z.string().catch(""),
    run_halt: RunHaltSchema.catch(EMPTY_RUN_HALT),
  })
  .loose();
export type RunsSummary = z.infer<typeof RunsSummarySchema>;

export const EMPTY_RUNS_SUMMARY: RunsSummary = {
  active: 0,
  queued: 0,
  running: 0,
  blocked: 0,
  completed_since: 0,
  failed_since: 0,
  cancelled_since: 0,
  cost_since_usd_ticks: 0,
  since: "",
  run_halt: EMPTY_RUN_HALT,
};

export const RunsResponseSchema = z
  .object({
    runs: z.array(RunSchema).catch([]).default([]),
    next_cursor: z.string().optional().catch(undefined),
    summary: RunsSummarySchema.catch(EMPTY_RUNS_SUMMARY),
  })
  .loose();
export type RunsResponse = z.infer<typeof RunsResponseSchema>;

export const EMPTY_RUNS_RESPONSE: RunsResponse = {
  runs: [],
  summary: EMPTY_RUNS_SUMMARY,
};

// outcome is open on the wire ("cancelled" | "already_over" | "not_found" |
// "error" today) — an unrecognized value must still show on its own row.
export const RunCancelOutcomeSchema = z
  .object({
    task_id: z.string().catch(""),
    outcome: z.string().catch("error"),
    error: z.string().optional().catch(undefined),
  })
  .loose();
export type RunCancelOutcome = z.infer<typeof RunCancelOutcomeSchema>;

export const CancelRunsResponseSchema = z
  .object({
    results: z.array(RunCancelOutcomeSchema).catch([]).default([]),
    cancelled: z.number().catch(0),
  })
  .loose();
export type CancelRunsResponse = z.infer<typeof CancelRunsResponseSchema>;

export const EMPTY_CANCEL_RUNS_RESPONSE: CancelRunsResponse = { results: [], cancelled: 0 };

export const KillSwitchResponseSchema = z
  .object({
    run_halt: RunHaltSchema.catch(EMPTY_RUN_HALT),
    cancelled: z.number().catch(0),
    results: z.array(RunCancelOutcomeSchema).catch([]).default([]),
  })
  .loose();
export type KillSwitchResponse = z.infer<typeof KillSwitchResponseSchema>;

export const EMPTY_KILL_SWITCH_RESPONSE: KillSwitchResponse = {
  run_halt: EMPTY_RUN_HALT,
  cancelled: 0,
  results: [],
};

// ---------------------------------------------------------------------------
// Pure helpers
// ---------------------------------------------------------------------------

/** Ticks per USD, shared with the runtimes usage math (COST_USD_TICKS_PER_USD). */
const RUN_COST_USD_TICKS_PER_USD = 10_000_000_000;

/** Raw ticks (as the API sends `cost_usd_ticks`) to a plain USD number. */
export function runCostUsd(costUsdTicks: number): number {
  return costUsdTicks / RUN_COST_USD_TICKS_PER_USD;
}

/** A running run with no reported activity for this long reads as "silent". */
export const RUN_SILENCE_THRESHOLD_MS = 90_000;

export function isRunSilent(run: Pick<Run, "status" | "silence_ms">): boolean {
  return run.status === "running" && run.silence_ms > RUN_SILENCE_THRESHOLD_MS;
}

/**
 * The issue-timeline element id a blocker's Decision Card scrolls to
 * (`#approval-<decision_id>`, set by the issue timeline card). `undefined`
 * when the blocker carries no decision — link to the issue plainly instead.
 */
export function blockerHash(blocker: RunBlocker | null | undefined): string | undefined {
  return blocker?.decision_id ? `#approval-${blocker.decision_id}` : undefined;
}
