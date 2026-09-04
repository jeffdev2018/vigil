import { z } from "zod";

export const BudgetPolicySchema = z.object({
  id: z.string().min(1),
  workspace_id: z.string().min(1),
  scope_type: z.enum(["workspace", "project", "agent"]),
  scope_id: z.string().nullable(),
  limit_usd_ticks: z.number().int().positive(),
  period: z.enum(["daily", "weekly", "monthly"]),
  warn_bps: z.number().int().min(0).max(10_000),
  action: z.enum(["observe", "enforce"]),
  revision: z.number().int().positive(),
  created_at: z.string(),
  updated_at: z.string(),
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
  id: z.string().min(1), policy_id: z.string().min(1),
  reason: z.string().min(1), expires_at: z.string(),
});

export type BudgetPolicy = z.infer<typeof BudgetPolicySchema>;
export type BudgetStatus = z.infer<typeof BudgetStatusSchema>;
export type BudgetOverride = z.infer<typeof BudgetOverrideSchema>;

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
