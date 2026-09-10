import type { FollowupBudget } from "../types";

/**
 * The daily caps on wake-ups, stored in `workspace.settings.followups` and
 * enforced server-side (service.FollowupSettingsFrom). The bounds and the
 * defaults are the server's — repeated here only so the form can refuse a
 * value the API would reject, not as a second source of truth.
 */
export const FOLLOWUP_SETTINGS_KEY = "followups";
export const FOLLOWUP_BUDGET_MIN = 1;
export const FOLLOWUP_BUDGET_MAX = 1000;

export const DEFAULT_FOLLOWUP_BUDGET: FollowupBudget = Object.freeze({
  max_per_agent_per_day: 20,
  max_per_workspace_per_day: 200,
}) as FollowupBudget;

/** Nearest allowed value; anything unreadable falls back to `fallback`. */
export function clampFollowupBudget(value: unknown, fallback: number): number {
  const n = typeof value === "string" ? Number(value.trim()) : value;
  if (typeof n !== "number" || !Number.isFinite(n)) return fallback;
  return Math.min(FOLLOWUP_BUDGET_MAX, Math.max(FOLLOWUP_BUDGET_MIN, Math.round(n)));
}

/**
 * The budget as `workspace.settings` holds it. `settings` is an opaque blob on
 * the wire, so every read is defensive: an absent, malformed or out-of-range
 * key reads as the server's own default rather than blanking the form.
 */
export function followupBudgetFromSettings(settings: unknown): FollowupBudget {
  const blob = (settings as Record<string, unknown> | null)?.[FOLLOWUP_SETTINGS_KEY];
  const raw = (blob ?? {}) as Record<string, unknown>;
  return {
    max_per_agent_per_day: clampFollowupBudget(
      raw["max_per_agent_per_day"],
      DEFAULT_FOLLOWUP_BUDGET.max_per_agent_per_day,
    ),
    max_per_workspace_per_day: clampFollowupBudget(
      raw["max_per_workspace_per_day"],
      DEFAULT_FOLLOWUP_BUDGET.max_per_workspace_per_day,
    ),
  };
}

/**
 * `PATCH /api/workspaces/{id}` replaces the whole settings blob, so a write
 * merges the follow-up key into what the workspace already carries — the same
 * shape `settings.doctrine.require_review` uses.
 */
export function mergeFollowupBudget(
  settings: unknown,
  budget: FollowupBudget,
): Record<string, unknown> {
  return {
    ...((settings as Record<string, unknown> | null) ?? {}),
    [FOLLOWUP_SETTINGS_KEY]: {
      max_per_agent_per_day: clampFollowupBudget(
        budget.max_per_agent_per_day,
        DEFAULT_FOLLOWUP_BUDGET.max_per_agent_per_day,
      ),
      max_per_workspace_per_day: clampFollowupBudget(
        budget.max_per_workspace_per_day,
        DEFAULT_FOLLOWUP_BUDGET.max_per_workspace_per_day,
      ),
    },
  };
}
