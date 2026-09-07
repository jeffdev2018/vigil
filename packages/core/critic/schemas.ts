import { z } from "zod";

// Adversarial critic (F25). One agent — or one squad — never delivers
// unreviewed: a critic on another provider reads the change and records a
// verdict. Two shapes cross the wire: the policy that asks for it, and the
// verdicts it produced.

export const CRITIC_VERDICTS = ["pass", "concerns", "block"] as const;
export type CriticVerdictValue = (typeof CRITIC_VERDICTS)[number];

/**
 * Reasons the PLATFORM wrote a verdict itself rather than a critic. A verdict
 * a critic actually wrote carries none.
 */
export const CRITIC_REASONS = [
  "no_distinct_provider",
  "max_rounds",
  "max_cost",
  "no_verdict",
] as const;
export type CriticReason = (typeof CRITIC_REASONS)[number] | "";

export const CriticFindingSchema = z
  .object({
    severity: z.enum(["bug", "warning", "info"]).catch("info"),
    file: z.string().catch(""),
    line: z.number().catch(0),
    title: z.string().catch(""),
    note: z.string().catch(""),
  })
  .loose();
export type CriticFinding = z.infer<typeof CriticFindingSchema>;

export const CriticVerdictSchema = z
  .object({
    id: z.string(),
    issue_id: z.string().catch(""),
    subject_task_id: z.string().catch(""),
    critic_task_id: z.string().nullish().catch(null),
    phase: z.string().catch("change"),
    // A verdict word this client does not know is `concerns`: it is the only
    // value that neither approves the delivery nor claims it was sent back.
    // An older client reading a newer server must never read an unknown word
    // as a pass.
    verdict: z.enum(CRITIC_VERDICTS).catch("concerns"),
    reason: z.string().catch(""),
    summary: z.string().catch(""),
    findings: z.array(CriticFindingSchema).catch([]).default([]),
    round: z.number().catch(1),
    cost_usd_ticks: z.number().catch(0),
    created_at: z.string().catch(""),
  })
  .loose();
export type CriticVerdict = z.infer<typeof CriticVerdictSchema>;

export const CriticVerdictListSchema = z
  .object({ verdicts: z.array(CriticVerdictSchema).catch([]).default([]) })
  .loose();
export type CriticVerdictList = z.infer<typeof CriticVerdictListSchema>;

export const EMPTY_CRITIC_VERDICTS: CriticVerdictList = { verdicts: [] };

export const CriticPolicySchema = z
  .object({
    subject_type: z.string().catch("agent"),
    subject_id: z.string().catch(""),
    enabled: z.boolean().catch(false),
    critic_agent_id: z.string().nullish().catch(null),
    require_distinct_provider: z.boolean().catch(true),
    blocking: z.boolean().catch(false),
    max_rounds: z.number().catch(1),
    max_cost_usd_ticks: z.number().nullish().catch(null),
    phases: z.array(z.string()).catch(["change"]).default(["change"]),
  })
  .loose();
export type CriticPolicy = z.infer<typeof CriticPolicySchema>;

/**
 * The fallback for an unreadable policy response is the feature OFF. Rendering
 * a switch as "on" when the server's answer could not be parsed would tell a
 * team its deliveries are being reviewed when nothing knows whether they are.
 */
export const EMPTY_CRITIC_POLICY: CriticPolicy = {
  subject_type: "agent",
  subject_id: "",
  enabled: false,
  critic_agent_id: null,
  require_distinct_provider: true,
  blocking: false,
  max_rounds: 1,
  max_cost_usd_ticks: null,
  phases: ["change"],
};

export interface CriticPolicyWrite {
  enabled: boolean;
  critic_agent_id?: string;
  require_distinct_provider?: boolean;
  blocking?: boolean;
  max_rounds?: number;
  max_cost_usd_ticks?: number | null;
  phases?: string[];
}

/**
 * What the UI must refuse to save, said once so the section and its test agree
 * with the server's 422. Empty string means the form is valid.
 */
export function criticPolicyError(
  policy: Pick<CriticPolicyWrite, "enabled" | "critic_agent_id">,
  subjectType: string,
  subjectId: string,
): "critic_required" | "critic_is_author" | "" {
  if (!policy.enabled) return "";
  const critic = (policy.critic_agent_id ?? "").trim();
  if (!critic) return "critic_required";
  if (subjectType === "agent" && critic === subjectId) return "critic_is_author";
  return "";
}

const TICKS_PER_USD = 1e10;

/** "$1.50" from a tick budget; empty for no cap. */
export function criticCostUsd(ticks: number | null | undefined): string {
  if (ticks === null || ticks === undefined || ticks <= 0) return "";
  const usd = ticks / TICKS_PER_USD;
  return `$${usd.toFixed(usd < 0.01 ? 4 : 2)}`;
}

/** Ticks from a dollar amount typed into the budget field; null clears the cap. */
export function criticCostTicks(usd: string): number | null {
  const value = Number.parseFloat(usd.trim());
  if (!Number.isFinite(value) || value <= 0) return null;
  return Math.round(value * TICKS_PER_USD);
}
