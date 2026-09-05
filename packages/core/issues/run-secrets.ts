import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

// Run-scoped secrets (K09): what each run of an issue received as a
// revocable token. Only keys and statuses exist client-side.

export type RunSecretStatus = "active" | "revoked" | "expired";

export interface RunSecret {
  id: string;
  task_id: string;
  key: string;
  status: RunSecretStatus;
  expires_at: string;
  revoked_at: string | null;
  revoke_reason: string | null;
  created_at: string;
}

export const runSecretKeys = {
  issue: (wsId: string, issueId: string) => ["issues", wsId, "run-secrets", issueId] as const,
};

export function issueRunSecretsOptions(wsId: string, issueId: string) {
  return queryOptions({ queryKey: runSecretKeys.issue(wsId, issueId), queryFn: () => api.listIssueRunSecrets(issueId), staleTime: 30_000 });
}

/** Secrets grouped by run, newest run first, keys in order. */
export function groupRunSecrets(secrets: RunSecret[]): { taskId: string; secrets: RunSecret[] }[] {
  const groups: { taskId: string; secrets: RunSecret[] }[] = [];
  for (const s of secrets) {
    const group = groups.find((g) => g.taskId === s.task_id);
    if (group) group.secrets.push(s);
    else groups.push({ taskId: s.task_id, secrets: [s] });
  }
  return groups;
}
