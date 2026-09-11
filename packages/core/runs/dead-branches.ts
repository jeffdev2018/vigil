import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { DeadBranchEntry } from "../api/schemas";
import type { Workspace } from "../types";

export type {
  DeadBranchEntry,
  DeadBranchPlan,
  DeadBranchDiscardResponse,
  DeadBranchDiscardSkipped,
  DeadBranchSkipReason,
} from "../api/schemas";

// Dead run-branch cleanup (JEF-388). Two halves live behind one workspace
// settings screen: an opt-in auto-GC policy (`branch_gc` key in the
// workspace's settings JSONB, patched through the plain workspace update —
// there is no dedicated endpoint) and a retroactive pass that lists dead
// branches and enqueues a human-confirmed batch discard. Discards are
// executed by each daemon asynchronously, so nothing here is optimistic: the
// plan query is re-read on settled and enqueued entries come back with
// skip_reason "action_pending".

export const BRANCH_GC_SETTINGS_KEY = "branch_gc";

export const BRANCH_GC_TTL_MIN = 1;
export const BRANCH_GC_TTL_MAX = 365;
export const BRANCH_GC_TTL_DEFAULT = 30;

export interface BranchGcPolicy {
  enabled: boolean;
  ttl_days: number;
}

export const DEFAULT_BRANCH_GC_POLICY: BranchGcPolicy = {
  enabled: false,
  ttl_days: BRANCH_GC_TTL_DEFAULT,
};

/**
 * Reads the `branch_gc` workspace-settings key defensively: the JSONB blob is
 * untyped, an older or hand-edited value must degrade to the off-by-default
 * policy rather than crash the settings screen. `enabled` fails closed; a
 * TTL outside 1..365 or non-numeric snaps back to the 30-day default.
 */
export function branchGcPolicy(workspace: Workspace | null | undefined): BranchGcPolicy {
  const settings = (workspace?.settings as Record<string, unknown> | null | undefined) ?? {};
  const raw = (settings[BRANCH_GC_SETTINGS_KEY] ?? {}) as Record<string, unknown>;
  const ttl =
    typeof raw.ttl_days === "number" && Number.isFinite(raw.ttl_days)
      ? Math.floor(raw.ttl_days)
      : BRANCH_GC_TTL_DEFAULT;
  return {
    enabled: raw.enabled === true,
    ttl_days:
      ttl >= BRANCH_GC_TTL_MIN && ttl <= BRANCH_GC_TTL_MAX ? ttl : BRANCH_GC_TTL_DEFAULT,
  };
}

export const deadBranchKeys = {
  plan: (wsId: string) => ["runs", wsId, "dead-branches"] as const,
};

/**
 * The retroactive-cleanup plan. No polling interval: the plan only changes
 * when a discard is enqueued (invalidated on settled) or when runs finish,
 * which the settings screen does not need to track live.
 */
export function deadBranchesOptions(wsId: string) {
  return queryOptions({
    queryKey: deadBranchKeys.plan(wsId),
    queryFn: () => api.listDeadBranches(),
    enabled: !!wsId,
  });
}

export function useDiscardDeadBranches(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ taskIds }: { taskIds: string[] }) => api.discardDeadBranches(taskIds),
    onSettled: () => qc.invalidateQueries({ queryKey: deadBranchKeys.plan(wsId) }),
  });
}

/**
 * Groups plan entries by runtime for display, first-seen order preserved so
 * the list is stable across refetches. The runtime's display name is the
 * grouping key the contract gives us (`runtime_name`); entries from runtimes
 * with identical names collapse into one group, which matches how a user
 * reads "machine".
 */
export function groupDeadBranchesByRuntime(
  entries: DeadBranchEntry[],
): { runtimeName: string; entries: DeadBranchEntry[] }[] {
  const groups = new Map<string, DeadBranchEntry[]>();
  for (const entry of entries) {
    const key = entry.runtime_name;
    const group = groups.get(key);
    if (group) group.push(entry);
    else groups.set(key, [entry]);
  }
  return [...groups.entries()].map(([runtimeName, groupEntries]) => ({
    runtimeName,
    entries: groupEntries,
  }));
}
