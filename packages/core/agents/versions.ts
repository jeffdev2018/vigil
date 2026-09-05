import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { workspaceKeys } from "../workspace/queries";

// Agent versions (K23): history is server state; a rollback invalidates the
// agent (its live config changed) and the history.

export const agentVersionKeys = {
  list: (wsId: string, agentId: string) => ["agent-versions", wsId, agentId] as const,
  diff: (wsId: string, agentId: string, versionId: string, against: string) => ["agent-versions", wsId, agentId, "diff", versionId, against] as const,
};

export function agentVersionsOptions(wsId: string, agentId: string) {
  return queryOptions({
    queryKey: agentVersionKeys.list(wsId, agentId),
    queryFn: () => api.listAgentVersions(agentId),
  });
}

export function agentVersionDiffOptions(wsId: string, agentId: string, versionId: string, against: string) {
  return queryOptions({
    queryKey: agentVersionKeys.diff(wsId, agentId, versionId, against),
    queryFn: () => api.getAgentVersionDiff(agentId, versionId, against),
  });
}

export function useRollbackAgentVersion(wsId: string, agentId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (versionId: string) => api.rollbackAgentVersion(agentId, versionId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: agentVersionKeys.list(wsId, agentId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.agent(wsId, agentId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    },
  });
}

export type DiffLine = { kind: "same" | "added" | "removed"; text: string };

/**
 * A line diff small enough to own: longest common subsequence over lines.
 * Instructions are a few hundred lines at most, so O(n·m) is fine.
 */
export function diffLines(before: string, after: string): DiffLine[] {
  const a = before === "" ? [] : before.split("\n");
  const b = after === "" ? [] : after.split("\n");
  const n = a.length;
  const m = b.length;
  const lcs: number[][] = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0));
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      lcs[i]![j] = a[i] === b[j] ? lcs[i + 1]![j + 1]! + 1 : Math.max(lcs[i + 1]![j]!, lcs[i]![j + 1]!);
    }
  }
  const out: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      out.push({ kind: "same", text: a[i]! });
      i++;
      j++;
    } else if (lcs[i + 1]![j]! >= lcs[i]![j + 1]!) {
      out.push({ kind: "removed", text: a[i]! });
      i++;
    } else {
      out.push({ kind: "added", text: b[j]! });
      j++;
    }
  }
  for (; i < n; i++) out.push({ kind: "removed", text: a[i]! });
  for (; j < m; j++) out.push({ kind: "added", text: b[j]! });
  return out;
}
