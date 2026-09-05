import { z } from "zod";

// BYOK model keys (K48). The key value never comes back: only a hint.

export const ModelKeyScopeSchema = z.enum(["workspace", "project"]);
export type ModelKeyScope = z.infer<typeof ModelKeyScopeSchema>;

export const ModelKeySchema = z.object({
  id: z.string(),
  workspace_id: z.string().catch(""),
  scope: ModelKeyScopeSchema.catch("workspace"),
  scope_id: z.string().nullable().catch(null),
  provider: z.string().catch(""),
  label: z.string().catch(""),
  key_hint: z.string().catch("***"),
  active: z.boolean().catch(false),
  priority: z.number().catch(0),
  deactivated_reason: z.string().catch(""),
  deactivated_at: z.string().nullable().catch(null),
  created_by: z.string().nullable().catch(null),
  created_at: z.string().catch(""),
  updated_at: z.string().catch(""),
});
export type ModelKey = z.infer<typeof ModelKeySchema>;

export const ModelKeyUsageSchema = z.object({
  model_key_id: z.string().catch(""),
  provider: z.string().catch(""),
  model: z.string().catch(""),
  task_count: z.number().catch(0),
  input_tokens: z.number().catch(0),
  output_tokens: z.number().catch(0),
  cache_read_tokens: z.number().catch(0),
  cache_write_tokens: z.number().catch(0),
  cost_usd_ticks: z.number().catch(0),
}).loose();
export type ModelKeyUsage = z.infer<typeof ModelKeyUsageSchema>;

export const ModelKeyVendorSchema = z.object({ id: z.string(), label: z.string().catch(""), env_var: z.string().catch("") }).loose();
export type ModelKeyVendor = z.infer<typeof ModelKeyVendorSchema>;

export const ModelKeyListSchema = z.object({
  keys: z.array(ModelKeySchema).catch([]).default([]),
  usage: z.array(ModelKeyUsageSchema).catch([]).default([]),
  vendors: z.array(ModelKeyVendorSchema).catch([]).default([]),
  configured: z.boolean().catch(false).default(false),
}).loose();
export type ModelKeyList = z.infer<typeof ModelKeyListSchema>;

export const EMPTY_MODEL_KEY_LIST: ModelKeyList = { keys: [], usage: [], vendors: [], configured: false };

export interface CreateModelKeyRequest {
  scope: ModelKeyScope;
  scope_id?: string;
  provider: string;
  label?: string;
  key: string;
  priority?: number;
  replace?: boolean;
}

/** Sum the usage rows of one key (all models) for a total per key. */
export function usageForKey(usage: ModelKeyUsage[], keyId: string): { tasks: number; tokens: number; costUsdTicks: number } {
  let tasks = 0;
  let tokens = 0;
  let costUsdTicks = 0;
  for (const u of usage) {
    if (u.model_key_id !== keyId) continue;
    tasks += u.task_count;
    tokens += u.input_tokens + u.output_tokens + u.cache_read_tokens + u.cache_write_tokens;
    costUsdTicks += u.cost_usd_ticks;
  }
  return { tasks, tokens, costUsdTicks };
}
