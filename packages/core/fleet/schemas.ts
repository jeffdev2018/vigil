import { z } from "zod";

// Fleet reads + agent consults (JEF-12). Schemas follow the repo's lenient
// discipline: strings that are enums server-side stay z.string() so an unknown
// value (a consult state a newer backend added) still parses and the caller
// renders it generically instead of falling back to empty.

export const FleetStatusRowSchema = z.object({
  agent_id: z.string().min(1),
  // Empty on the restricted-agents fold bucket; absent on older backends
  // (omitempty) — normalize to "".
  name: z.string().optional().default(""),
  running_task_count: z.number().int().nonnegative(),
  task_count: z.number().int().nonnegative(),
  failed_count: z.number().int().nonnegative(),
});
export const FleetStatusListSchema = z.array(FleetStatusRowSchema);

export const FleetCostRowSchema = z.object({
  agent_id: z.string().min(1),
  // Pricing ticks: 1e10 ticks = 1 USD. Convert before displaying.
  cost_usd_ticks: z.number().int().nonnegative(),
  input_tokens: z.number().int().nonnegative(),
  output_tokens: z.number().int().nonnegative(),
  task_count: z.number().int().nonnegative(),
});
export const FleetCostListSchema = z.array(FleetCostRowSchema);

export const FleetHistoryRowSchema = z.object({
  date: z.string().min(1),
  agent_id: z.string().min(1),
  task_count: z.number().int().nonnegative(),
  failed_count: z.number().int().nonnegative(),
});
export const FleetHistoryListSchema = z.array(FleetHistoryRowSchema);

// One consult row. Cost/tokens are nullable because the LLM layer does not
// report usage on this path — NULL means "not reported", never an estimate.
// `state` stays a plain string: pending | answered | failed | refused today,
// and an unknown value must still parse (rendered as a generic line).
export const AgentConsultSchema = z.object({
  consult_id: z.string().min(1),
  task_id: z.string().min(1),
  agent_id: z.string().min(1),
  model: z.string(),
  question: z.string(),
  answer: z.string().nullable(),
  state: z.string(),
  refusal_reason: z.string().nullable(),
  input_tokens: z.number().int().nonnegative().nullable(),
  output_tokens: z.number().int().nonnegative().nullable(),
  cost_usd_ticks: z.number().int().nonnegative().nullable(),
  created_at: z.string(),
  finalized_at: z.string().nullable().optional(),
});
export const AgentConsultListSchema = z.array(AgentConsultSchema);

export type FleetStatusRow = z.infer<typeof FleetStatusRowSchema>;
export type FleetCostRow = z.infer<typeof FleetCostRowSchema>;
export type FleetHistoryRow = z.infer<typeof FleetHistoryRowSchema>;
export type AgentConsult = z.infer<typeof AgentConsultSchema>;
