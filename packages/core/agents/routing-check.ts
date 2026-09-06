import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";

// Workflow execution safety (JEF-275).
//
// Two settings live here because they answer the same question from opposite
// ends: routing-check says whether a trigger for this agent would run at all,
// and the workflow limits say how far a run that did start may cascade.

/** One reason a trigger for an agent might not run. */
export interface RoutingProblem {
  code: string;
  message: string;
  /** A problem no amount of waiting resolves: the trigger is refused. */
  fatal: boolean;
}

export interface RoutingCheck {
  agent_id: string;
  ok: boolean;
  fatal: boolean;
  problems: RoutingProblem[];
}

/** How far one workflow — the run plus every leg it spawns — may grow. */
export interface WorkflowLimits {
  max_legs: number;
  /** 0 means the workflow is bounded by leg count only. */
  max_cost_usd_ticks: number;
}

/** The setting plus the range the server accepts, so the form has one source. */
export interface WorkflowLimitsSettings extends WorkflowLimits {
  min_legs: number;
  max_legs_allowed: number;
}

export const routingCheckKeys = {
  agent: (wsId: string, agentId: string) => ["routing-check", wsId, agentId] as const,
  workflowLimits: (wsId: string) => ["workflow-limits", wsId] as const,
};

export function agentRoutingCheckOptions(wsId: string, agentId: string) {
  return queryOptions({
    queryKey: routingCheckKeys.agent(wsId, agentId),
    queryFn: () => api.getAgentRoutingCheck(agentId),
    enabled: agentId.length > 0,
  });
}

export function workflowLimitsOptions(wsId: string) {
  return queryOptions({ queryKey: routingCheckKeys.workflowLimits(wsId), queryFn: () => api.getWorkflowLimits() });
}

export function useSaveWorkflowLimits(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: WorkflowLimits) => api.putWorkflowLimits(v),
    onSettled: () => qc.invalidateQueries({ queryKey: routingCheckKeys.workflowLimits(wsId) }),
  });
}

/**
 * The one line the agent page shows. A fatal problem is what the reader must
 * act on, so it wins over any number of warnings; several warnings collapse to
 * the first plus a count, because a routing check is a status line, not a list.
 */
export function routingCheckSummary(check: RoutingCheck | undefined): {
  tone: "ok" | "warning" | "error";
  message: string;
  extra: number;
} | null {
  if (!check) return null;
  if (check.problems.length === 0) return { tone: "ok", message: "", extra: 0 };
  const fatal = check.problems.find((p) => p.fatal);
  const shown = fatal ?? check.problems[0]!;
  return {
    tone: fatal ? "error" : "warning",
    message: shown.message,
    extra: check.problems.length - 1,
  };
}
