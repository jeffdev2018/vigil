import { z } from "zod";

export const BudgetPolicySchema = z.object({
  // Identity fields stay strict: a malformed id/workspace_id means the whole
  // record is garbage and must be rejected (see the list-schema test below),
  // not silently repackaged as a fake-looking real policy.
  id: z.string().min(1),
  workspace_id: z.string().min(1),
  scope_type: z.enum(["workspace", "project", "agent"]).catch("workspace"),
  scope_id: z.string().nullable().catch(null),
  limit_usd_ticks: z.number().int().positive().catch(0),
  period: z.enum(["daily", "weekly", "monthly"]).catch("daily"),
  warn_bps: z.number().int().min(0).max(10_000).catch(0),
  action: z.enum(["observe", "enforce"]).catch("observe"),
  revision: z.number().int().positive().catch(1),
  created_at: z.string().catch(""),
  updated_at: z.string().catch(""),
});

export const BudgetPolicyListSchema = z.array(BudgetPolicySchema);

export const BudgetStatusSchema = z.object({
  policy: BudgetPolicySchema,
  spent_usd_ticks: z.number().int().nonnegative(),
  reserved_usd_ticks: z.number().int().nonnegative(),
  period_start: z.string(),
  period_end: z.string(),
  reached: z.boolean(),
  override_expires_at: z.string().nullable(),
});

export const BudgetStatusListSchema = z.array(BudgetStatusSchema);
export const BudgetOverrideSchema = z.object({
  // Same identity-vs-attribute split as BudgetPolicySchema above.
  id: z.string().min(1), policy_id: z.string().min(1),
  reason: z.string().min(1).catch(""), expires_at: z.string().catch(""),
});

export type BudgetPolicy = z.infer<typeof BudgetPolicySchema>;
export type BudgetStatus = z.infer<typeof BudgetStatusSchema>;
export type BudgetOverride = z.infer<typeof BudgetOverrideSchema>;

/** Used as the parseWithFallback default when a mutation response fails validation entirely. */
export const EMPTY_BUDGET_POLICY: BudgetPolicy = {
  id: "", workspace_id: "", scope_type: "workspace", scope_id: null, limit_usd_ticks: 0,
  period: "daily", warn_bps: 0, action: "observe", revision: 1, created_at: "", updated_at: "",
};
export const EMPTY_BUDGET_OVERRIDE: BudgetOverride = { id: "", policy_id: "", reason: "", expires_at: "" };

export interface CreateBudgetPolicyRequest {
  scope_type: BudgetPolicy["scope_type"];
  scope_id?: string | null;
  limit_usd_ticks: number;
  period: BudgetPolicy["period"];
  warn_bps?: number;
  action?: BudgetPolicy["action"];
}

export interface UpdateBudgetPolicyRequest {
  limit_usd_ticks: number;
  period: BudgetPolicy["period"];
  warn_bps: number;
  action: BudgetPolicy["action"];
  revision: number;
}
