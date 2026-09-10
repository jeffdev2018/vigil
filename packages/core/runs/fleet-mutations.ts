import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { runHaltKeys } from "../run-halt";
import { approvalKeys } from "../approvals/queries";
import { runKeys } from "./fleet-queries";

/**
 * Cancel one or more runs. Never optimistic: the server reports one outcome
 * per run (cancelled / already_over / not_found / error) and the caller
 * needs the real per-row answer, not a guess this client rolls back later.
 */
export function useCancelRuns(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (taskIds: string[]) => api.cancelRuns(taskIds),
    onSettled: () => qc.invalidateQueries({ queryKey: runKeys.all(wsId) }),
  });
}

/**
 * Owner/admin only server-side. Halts the fleet then cancels every run not
 * already over, in that order. Refreshes the halt (shared with the approvals
 * feed and the banner) and the runs list alongside.
 */
export function useKillSwitch(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (reason: string) => api.killSwitch(reason),
    onSuccess: (data) => qc.setQueryData(runHaltKeys.all(wsId), data.run_halt),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: runKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: approvalKeys.all(wsId) });
    },
  });
}
