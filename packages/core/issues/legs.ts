import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

// Per-leg accounting (JEF-274): a multi-leg workflow — draft, review,
// revision, retry, fallback, escalation — is a set of separate runs, each with
// its own usage. These types put them back together so a surface can answer
// "what did this piece of work actually cost", which no single run can.

export interface WorkflowLeg {
  task_id: string;
  /** `draft` on the primary leg; the server never sends an empty role here. */
  leg_role: string;
  status: string;
  agent_id: string;
  agent_name: string;
  runtime_id: string;
  runtime_name: string;
  provider: string;
  model: string;
  input_tokens: number;
  output_tokens: number;
  cost_usd_ticks: number;
  duration_seconds: number;
  created_at: string | null;
  completed_at: string | null;
}

export interface WorkflowLegTotals {
  legs: number;
  cost_usd_ticks: number;
  input_tokens: number;
  output_tokens: number;
  duration_seconds: number;
}

export interface WorkflowLegs {
  root_task_id: string;
  legs: WorkflowLeg[];
  totals: WorkflowLegTotals;
}

export const legKeys = {
  workflow: (wsId: string, rootTaskId: string) => ["task-legs", wsId, rootTaskId] as const,
};

/**
 * The workflow one run belongs to. Any leg's id resolves to the same answer —
 * the server resolves the root first — so a caller holding a review or a retry
 * does not need to find the draft.
 */
export function taskLegsOptions(wsId: string, taskId: string) {
  return queryOptions({
    queryKey: legKeys.workflow(wsId, taskId),
    queryFn: () => api.getTaskLegs(taskId),
    staleTime: 30_000,
  });
}

/**
 * Locale key under `issues:legs.role` for one leg role. Unknown roles — a
 * newer backend added a producer this client does not know — fall back to
 * `other`, never to the raw server string.
 */
const KNOWN_LEG_ROLES = new Set([
  "draft",
  "retry",
  "fallback",
  "rerun",
  "review",
  "critique",
  "answer",
  "revision",
  "watchdog",
  "duel",
  "fanout",
  "shard",
  "eval",
  "escalation",
]);

export function legRoleLabelKey(role: string): string {
  return KNOWN_LEG_ROLES.has(role) ? role : "other";
}

/** The workflow's root, from any leg. Empty when the run belongs to none. */
export function workflowRootOf(task: { id: string; leg_role?: string; workflow_root_task_id?: string }): string {
  if (!task.leg_role) return "";
  return task.workflow_root_task_id || task.id;
}
