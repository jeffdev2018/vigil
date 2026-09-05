import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { issueKeys } from "./queries";

// Undo for agent actions (K69): the side effects a run produced on an
// issue, grouped by run, each reversible within the workspace's window; a
// whole run or one effect can be reversed; the breaker lowers an agent's
// trust mode when too many of its runs are undone in a day.

export type AgentEffectKind =
  | "issue_status"
  | "issue_field"
  | "comment_create"
  | "comment_update"
  | "note_create"
  | "note_update"
  | "note_archive"
  | "triage_verdict"
  | "issue_create";

export interface AgentEffect {
  id: string;
  task_id: string;
  agent_id: string;
  agent_name: string;
  issue_id: string | null;
  kind: string;
  target_type: string;
  target_id: string;
  before: Record<string, unknown>;
  after: Record<string, unknown>;
  reversible: boolean;
  /** applied | pending | approved | rejected (held writes of a preview-mode run) */
  status: string;
  decision_id: string | null;
  payload: Record<string, unknown>;
  reversed_at: string | null;
  reversed_by_type: string | null;
  reverse_error: string | null;
  within_window: boolean;
  expires_at: string;
  created_at: string;
}

export interface AgentEffectList {
  effects: AgentEffect[];
  window_hours: number;
}

export interface UndoReport {
  reversed: number;
  skipped: { id: string; kind: string; reason: string }[];
  breaker: { tripped: boolean; trust_mode: string };
  effects: AgentEffect[];
}

export interface UndoSettings {
  window_hours: number;
  breaker_threshold: number;
}

export const agentEffectKeys = {
  issue: (wsId: string, issueId: string) => ["agent-effects", wsId, issueId] as const,
  settings: (wsId: string) => ["agent-effects", wsId, "settings"] as const,
};

export function issueAgentEffectsOptions(wsId: string, issueId: string) {
  return queryOptions({ queryKey: agentEffectKeys.issue(wsId, issueId), queryFn: () => api.listIssueAgentEffects(issueId) });
}

export function useUndoTask(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (taskId: string) => api.undoTask(taskId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: agentEffectKeys.issue(wsId, issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

export function useUndoAgentEffect(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (effectId: string) => api.undoAgentEffect(effectId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: agentEffectKeys.issue(wsId, issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

export function undoSettingsOptions(wsId: string) {
  return queryOptions({ queryKey: agentEffectKeys.settings(wsId), queryFn: () => api.getUndoSettings() });
}

export function useSaveUndoSettings(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: UndoSettings) => api.putUndoSettings(v),
    onSettled: () => qc.invalidateQueries({ queryKey: agentEffectKeys.settings(wsId) }),
  });
}

export interface AgentEffectRun {
  task_id: string;
  agent_name: string;
  effects: AgentEffect[];
  /** effects that an undo of the run would reverse now */
  pending: number;
}

/** Group a newest-first effect list by run, keeping the list order. */
export function groupEffectsByRun(effects: AgentEffect[]): AgentEffectRun[] {
  const runs: AgentEffectRun[] = [];
  const byTask = new Map<string, AgentEffectRun>();
  for (const e of effects) {
    let run = byTask.get(e.task_id);
    if (!run) {
      run = { task_id: e.task_id, agent_name: e.agent_name, effects: [], pending: 0 };
      byTask.set(e.task_id, run);
      runs.push(run);
    }
    run.effects.push(e);
    if (effectState(e) === "pending") run.pending += 1;
  }
  return runs;
}

export type AgentEffectState = "pending" | "reversed" | "expired" | "not_reversible" | "failed" | "held" | "approved" | "rejected";

/** pending = can be reversed now; held / approved / rejected for a preview-mode write; reversed / expired / not_reversible / failed otherwise. */
export function effectState(e: AgentEffect): AgentEffectState {
  if (e.status === "pending") return "held";
  if (e.status === "approved") return "approved";
  if (e.status === "rejected") return "rejected";
  if (e.reversed_at) return "reversed";
  if (!e.reversible) return "not_reversible";
  if (e.reverse_error) return "failed";
  if (!e.within_window) return "expired";
  return "pending";
}

/** The field an issue_field effect touched, for the label. */
export function effectField(e: AgentEffect): string {
  const f = e.before["field"] ?? e.after["field"];
  return typeof f === "string" ? f : "";
}
