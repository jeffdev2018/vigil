import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";

// Workflow selector (JEF-273): the backend picks an execution strategy
// (single / cascade / critique) per task, learned from the run history.

export interface WorkflowPolicySettings {
  /** "auto" learns the workflow from history; "off" always runs single. */
  mode: "off" | "auto";
}

export const workflowPolicyKeys = {
  settings: (wsId: string) => ["workflow-policy", wsId, "settings"] as const,
};

export function workflowPolicySettingsOptions(wsId: string) {
  return queryOptions({ queryKey: workflowPolicyKeys.settings(wsId), queryFn: () => api.getWorkflowPolicySettings() });
}

export function useSaveWorkflowPolicySettings(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: WorkflowPolicySettings) => api.putWorkflowPolicySettings(v),
    onSettled: () => qc.invalidateQueries({ queryKey: workflowPolicyKeys.settings(wsId) }),
  });
}

// The selection reason ("policy:auto" | "policy:off-default" |
// "auto:insufficient-data" | a newer token) rides only the
// task:workflow-selected event, not the task payload, so the run-info popover
// keeps the latest one per task in memory. The map is bounded: past the cap
// the oldest entries fall out, which is fine — transcripts for those runs
// were closed long ago.
const MAX_WORKFLOW_SELECTIONS = 200;
const workflowSelections = new Map<string, string>();

export function rememberWorkflowSelection(taskId: string, reason: string): void {
  if (!taskId || !reason) return;
  // Re-insert so the entry becomes the newest for the eviction order.
  workflowSelections.delete(taskId);
  workflowSelections.set(taskId, reason);
  if (workflowSelections.size > MAX_WORKFLOW_SELECTIONS) {
    const oldest = workflowSelections.keys().next().value;
    if (oldest !== undefined) workflowSelections.delete(oldest);
  }
}

export function workflowSelectionReason(taskId: string): string | undefined {
  return workflowSelections.get(taskId);
}

export function clearWorkflowSelections(): void {
  workflowSelections.clear();
}
