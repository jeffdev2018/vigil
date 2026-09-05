import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

// Workspace export / import (K76): runs history and the template catalog.

export const transferKeys = {
  runs: (wsId: string) => ["workspace-transfer-runs", wsId] as const,
  templates: () => ["workspace-templates"] as const,
};

export function transferRunsOptions(wsId: string) {
  return queryOptions({ queryKey: transferKeys.runs(wsId), queryFn: () => api.listWorkspaceTransferRuns(), enabled: !!wsId });
}

export function workspaceTemplatesOptions() {
  return queryOptions({ queryKey: transferKeys.templates(), queryFn: () => api.listWorkspaceTemplates() });
}

/** Hands the exported zip to the browser as a download. */
export function saveTransferBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}
