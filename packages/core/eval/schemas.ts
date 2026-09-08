import { z } from "zod";

// Eval Lab (K24): a resolved issue whose acceptance criteria are all proved
// becomes a reusable eval case. A suite of cases is replayed against one
// agent version; the run scores each case on the criteria it proves again.

export type EvalRunStatus = "running" | "completed" | "failed";
export type EvalRunCaseStatus = "pending" | "passed" | "failed" | "infra_failed";

export const EvalCaseCriterionSchema = z.object({
  id: z.string().catch(""),
  text: z.string().catch(""),
  proof_type: z.string().catch(""),
  proof_ref: z.string().catch(""),
  proof_state: z.string().catch(""),
}).loose();
export type EvalCaseCriterion = z.infer<typeof EvalCaseCriterionSchema>;

export const EvalCaseSchema = z.object({
  id: z.string(),
  workspace_id: z.string().catch(""),
  source_issue_id: z.string().catch(""),
  source_issue_number: z.number().catch(0),
  title: z.string().catch(""),
  description: z.string().catch(""),
  criteria: z.array(EvalCaseCriterionSchema).catch([]).default([]),
  created_by: z.string().nullable().catch(null),
  created_at: z.string().catch(""),
}).loose();
export type EvalCase = z.infer<typeof EvalCaseSchema>;

export const EvalSuiteSchema = z.object({
  id: z.string(),
  workspace_id: z.string().catch(""),
  name: z.string().catch(""),
  case_ids: z.array(z.string()).catch([]).default([]),
  case_count: z.number().catch(0),
  created_by: z.string().nullable().catch(null),
  created_at: z.string().catch(""),
  updated_at: z.string().catch(""),
}).loose();
export type EvalSuite = z.infer<typeof EvalSuiteSchema>;

// Statuses stay `z.string()` with a safe `.catch()` default so an unknown
// value from a newer server still parses; the UI switches carry a default
// branch. The exported TS types are the narrow unions.
export const EvalRunCaseSchema = z.object({
  case_id: z.string().catch(""),
  case_title: z.string().catch(""),
  issue_id: z.string().catch(""),
  task_id: z.string().catch(""),
  status: z.string().catch("pending"),
  score: z.number().nullable().catch(null),
  detail: z.string().catch(""),
  settled_at: z.string().nullable().catch(null),
}).loose();

export const EvalRunSchema = z.object({
  id: z.string(),
  workspace_id: z.string().catch(""),
  suite_id: z.string().catch(""),
  suite_name: z.string().catch(""),
  agent_id: z.string().catch(""),
  agent_version_id: z.string().catch(""),
  agent_version_number: z.number().catch(0),
  status: z.string().catch("running"),
  score: z.number().nullable().catch(null),
  started_by: z.string().nullable().catch(null),
  started_at: z.string().catch(""),
  completed_at: z.string().nullable().catch(null),
  cases: z.array(EvalRunCaseSchema).catch([]).default([]),
}).loose();

export interface EvalRunCase {
  case_id: string;
  case_title: string;
  issue_id: string;
  task_id: string;
  status: EvalRunCaseStatus;
  score: number | null;
  detail: string;
  settled_at: string | null;
}

export interface EvalRun {
  id: string;
  workspace_id: string;
  suite_id: string;
  suite_name: string;
  agent_id: string;
  agent_version_id: string;
  agent_version_number: number;
  status: EvalRunStatus;
  score: number | null;
  started_by: string | null;
  started_at: string;
  completed_at: string | null;
  cases: EvalRunCase[];
}

export const EvalCaseEnvelopeSchema = z.object({ case: EvalCaseSchema.nullable().catch(null) }).loose();
export const EvalCaseListSchema = z.object({ cases: z.array(EvalCaseSchema).catch([]).default([]) }).loose();
export const EvalSuiteEnvelopeSchema = z.object({ suite: EvalSuiteSchema.nullable().catch(null) }).loose();
export const EvalSuiteListSchema = z.object({ suites: z.array(EvalSuiteSchema).catch([]).default([]) }).loose();
export const EvalRunEnvelopeSchema = z.object({ run: EvalRunSchema.nullable().catch(null) }).loose();
export const EvalRunListSchema = z.object({ runs: z.array(EvalRunSchema).catch([]).default([]) }).loose();

export interface CreateEvalSuiteRequest {
  name: string;
  case_ids: string[];
}

export interface RunEvalSuiteRequest {
  agent_id: string;
  agent_version_id: string;
}

export type ScoreTone = "success" | "warning" | "destructive";

/** Colour band for an eval score: 80+ is a pass, 50+ is shaky, below is a regression. */
export function evalScoreTone(score: number | null | undefined): ScoreTone {
  if (typeof score !== "number" || Number.isNaN(score)) return "destructive";
  if (score >= 80) return "success";
  if (score >= 50) return "warning";
  return "destructive";
}

/** The suite's newest run, or undefined when it has never been run. */
export function latestRunForSuite(runs: EvalRun[], suiteId: string): EvalRun | undefined {
  return runs.find((r) => r.suite_id === suiteId);
}

/** A suite with a run in flight cannot be launched again (server answers 409). */
export function hasRunningRun(runs: EvalRun[], suiteId: string): boolean {
  return runs.some((r) => r.suite_id === suiteId && r.status === "running");
}

// Internal benchmark harness (JEF-276). A benchmark replays ONE suite against
// several (runtime, model) candidates at once, so the only difference between
// the numbers is the policy under test. Each candidate gets its own eval run
// with the pair pinned on it, hence a benchmark run is an eval run plus the
// pin, the per-class breakdown and the delta against a baseline run.

export const BenchmarkClassBreakdownSchema = z.object({
  cases: z.number().catch(0),
  passed: z.number().catch(0),
  score: z.number().nullable().catch(null),
  // A provider that never priced a run and a run that never started are not
  // zero, so the server answers null rather than 0.
  cost_usd_ticks: z.number().nullable().catch(null),
  duration_seconds: z.number().nullable().catch(null),
}).loose();

export const BenchmarkRunSchema = EvalRunSchema.extend({
  benchmark: z.boolean().catch(false),
  runtime_id: z.string().catch(""),
  runtime_name: z.string().catch(""),
  model: z.string().catch(""),
  baseline_run_id: z.string().nullable().catch(null),
  // Keys are the router's raw task-class tokens; an unknown one from a newer
  // backend must still parse, so this stays a record and never an enum.
  per_class: z.record(z.string(), BenchmarkClassBreakdownSchema).catch({}).default({}),
  delta_score: z.number().nullable().catch(null),
});

export const BenchmarkRunListSchema = z.object({
  runs: z.array(BenchmarkRunSchema).catch([]).default([]),
}).loose();

export const BenchmarkCorpusClassSchema = z.object({
  count: z.number().catch(0),
  share: z.number().catch(0),
}).loose();

export const BenchmarkCorpusSchema = z.object({
  suite_id: z.string().catch(""),
  suite_name: z.string().catch(""),
  cases: z.number().catch(0),
  classes: z.record(z.string(), BenchmarkCorpusClassSchema).catch({}).default({}),
  // The server reports a too-small suite as balanced: the question does not
  // apply to it.
  balanced: z.boolean().catch(true),
}).loose();

export const BenchmarkPolicySchema = z.object({
  cost_weight: z.number().catch(0),
  duration_weight: z.number().catch(0),
  min_samples: z.number().catch(0),
}).loose();

export const BenchmarkPolicyPickSchema = z.object({
  task_class: z.string().catch(""),
  run_id: z.string().catch(""),
  runtime_id: z.string().catch(""),
  model: z.string().catch(""),
  score: z.number().catch(0),
  cases: z.number().catch(0),
  passed: z.number().catch(0),
  avg_cost_usd: z.number().nullable().catch(null),
}).loose();

const EMPTY_POLICY = { cost_weight: 0, duration_weight: 0, min_samples: 0 };

export const BenchmarkPolicyOutcomeSchema = z.object({
  policy: BenchmarkPolicySchema.catch(EMPTY_POLICY),
  baseline: z.boolean().catch(false),
  scored_classes: z.number().catch(0),
  cases: z.number().catch(0),
  passed: z.number().catch(0),
  passed_rate: z.number().catch(0),
  avg_cost_usd: z.number().nullable().catch(null),
  picks: z.array(BenchmarkPolicyPickSchema).catch([]).default([]),
}).loose();

const EMPTY_OUTCOME = {
  policy: EMPTY_POLICY, baseline: false, scored_classes: 0, cases: 0, passed: 0,
  passed_rate: 0, avg_cost_usd: null, picks: [],
};

export const BenchmarkPolicySearchSchema = z.object({
  grid: z.array(BenchmarkPolicyOutcomeSchema).catch([]).default([]),
  baseline: BenchmarkPolicyOutcomeSchema.catch(EMPTY_OUTCOME),
  winner: BenchmarkPolicyOutcomeSchema.catch(EMPTY_OUTCOME),
  improved: z.boolean().catch(false),
}).loose();

export interface BenchmarkClassBreakdown {
  cases: number;
  passed: number;
  score: number | null;
  cost_usd_ticks: number | null;
  duration_seconds: number | null;
}

export interface BenchmarkRun extends EvalRun {
  benchmark: boolean;
  runtime_id: string;
  runtime_name: string;
  model: string;
  baseline_run_id: string | null;
  per_class: Record<string, BenchmarkClassBreakdown>;
  delta_score: number | null;
}

export interface BenchmarkCorpusClass {
  count: number;
  /** 0..1 share of the suite's cases. */
  share: number;
}

export interface BenchmarkCorpus {
  suite_id: string;
  suite_name: string;
  cases: number;
  classes: Record<string, BenchmarkCorpusClass>;
  balanced: boolean;
}

export interface BenchmarkPolicy {
  cost_weight: number;
  duration_weight: number;
  min_samples: number;
}

export interface BenchmarkPolicyPick {
  task_class: string;
  run_id: string;
  runtime_id: string;
  model: string;
  score: number;
  cases: number;
  passed: number;
  avg_cost_usd: number | null;
}

export interface BenchmarkPolicyOutcome {
  policy: BenchmarkPolicy;
  baseline: boolean;
  scored_classes: number;
  cases: number;
  passed: number;
  passed_rate: number;
  avg_cost_usd: number | null;
  picks: BenchmarkPolicyPick[];
}

export interface BenchmarkPolicySearch {
  grid: BenchmarkPolicyOutcome[];
  baseline: BenchmarkPolicyOutcome;
  winner: BenchmarkPolicyOutcome;
  improved: boolean;
}

/** One (runtime, model) pair to replay the suite against. */
export interface BenchmarkCandidate {
  runtime_id: string;
  model: string;
}

export interface RunBenchmarkRequest {
  agent_id: string;
  agent_version_id: string;
  candidates: BenchmarkCandidate[];
  baseline_run_id?: string;
}

export interface BenchmarkPolicySearchRequest {
  runs: string[];
}

/**
 * Colour band for a delta against the baseline run. Unlike a score, 0 is not a
 * failure — it is "no movement", which reads as a warning, same as a delta the
 * server could not compute yet.
 */
export function benchmarkDeltaTone(delta: number | null | undefined): ScoreTone {
  if (typeof delta !== "number" || Number.isNaN(delta)) return "warning";
  if (delta > 0) return "success";
  if (delta === 0) return "warning";
  return "destructive";
}

/**
 * A suite's class mix, heaviest class first and ties broken by class name so
 * the order is stable across refetches and the UI never has to resort.
 */
export function sortedCorpusClasses(
  corpus: BenchmarkCorpus | null | undefined,
): [string, BenchmarkCorpusClass][] {
  return Object.entries(corpus?.classes ?? {}).toSorted(
    ([aClass, a], [bClass, b]) => b.count - a.count || aClass.localeCompare(bClass),
  );
}
