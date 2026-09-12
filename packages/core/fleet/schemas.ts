import { z } from "zod";

// Agent consults (JEF-12). Schemas follow the repo's lenient
// discipline: strings that are enums server-side stay z.string() so an unknown
// value (a consult state a newer backend added) still parses and the caller
// renders it generically instead of falling back to empty.

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

export type AgentConsult = z.infer<typeof AgentConsultSchema>;
