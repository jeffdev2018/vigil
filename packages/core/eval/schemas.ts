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
