import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { Cycle, CycleWriteRequest } from "../types";
import { issueKeys } from "../issues/queries";

// Dated cycles (F29): the list per project, one cycle's detail, its burndown,
// and the writes that plan work into it.

export const cycleKeys = {
  all: (wsId: string) => ["cycles", wsId] as const,
  list: (wsId: string, projectId?: string) =>
    [...cycleKeys.all(wsId), "list", projectId ?? "all"] as const,
  detail: (wsId: string, id: string) => [...cycleKeys.all(wsId), "detail", id] as const,
  burndown: (wsId: string, id: string) => [...cycleKeys.all(wsId), "burndown", id] as const,
};

export function cycleListOptions(wsId: string, projectId?: string) {
  return queryOptions({
    queryKey: cycleKeys.list(wsId, projectId),
    queryFn: () => api.listCycles({ projectId }),
    select: (data) => data.cycles,
  });
}

export function cycleDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: cycleKeys.detail(wsId, id),
    queryFn: () => api.getCycle(id),
    enabled: id.length > 0,
  });
}

export function cycleBurndownOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: cycleKeys.burndown(wsId, id),
    queryFn: () => api.getCycleBurndown(id),
    enabled: id.length > 0,
  });
}

export function useCreateCycle(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: CycleWriteRequest) => api.createCycle(data),
    onSettled: () => qc.invalidateQueries({ queryKey: cycleKeys.all(wsId) }),
  });
}

export function useUpdateCycle(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; data: CycleWriteRequest }) => api.updateCycle(v.id, v.data),
    onSettled: () => qc.invalidateQueries({ queryKey: cycleKeys.all(wsId) }),
  });
}

export function useDeleteCycle(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteCycle(id),
    // Deleting a cycle detaches its issues, so their rows are stale too.
    onSettled: () => {
      qc.invalidateQueries({ queryKey: cycleKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

export function useCloseCycle(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.closeCycle(id),
    // Closing runs the rollover, which moves issues between cycles.
    onSettled: () => {
      qc.invalidateQueries({ queryKey: cycleKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

/** Done ratio in [0, 1]; 0 without issues. Mirrors goalProgress. */
export function cycleProgress(cycle: Pick<Cycle, "issue_count" | "done_count">): number {
  return cycle.issue_count > 0 ? Math.min(1, cycle.done_count / cycle.issue_count) : 0;
}

/**
 * Fill ratio of one capacity bar in [0, 1], plus whether it overflows.
 *
 * An undeclared capacity has no bar to fill: it returns ratio 0 and
 * `declared: false` so the UI shows the load as a number instead of drawing a
 * bar that would imply a limit nobody set.
 */
export function capacityFill(side: { capacity: number | null; load: number }): {
  declared: boolean;
  ratio: number;
  over: boolean;
} {
  if (side.capacity === null || side.capacity <= 0) {
    return { declared: false, ratio: 0, over: false };
  }
  return {
    declared: true,
    ratio: Math.min(1, side.load / side.capacity),
    over: side.load > side.capacity,
  };
}

/** Cycles of one status, newest start first — the page's three sections. */
export function cyclesByStatus(cycles: Cycle[], status: Cycle["status"]): Cycle[] {
  return cycles.filter((c) => c.status === status);
}

// --- Goal progress (F29 initiatives) ---------------------------------------
//
// Lives here rather than under goals/ because it is the aggregate this feature
// added to goals, not part of the goal tree K74 already owns.

export const goalProgressKeys = {
  detail: (wsId: string, goalId: string) => ["goals", wsId, "progress", goalId] as const,
};

export function goalProgressOptions(wsId: string, goalId: string) {
  return queryOptions({
    queryKey: goalProgressKeys.detail(wsId, goalId),
    queryFn: () => api.getGoalProgress(goalId),
    enabled: goalId.length > 0,
  });
}
