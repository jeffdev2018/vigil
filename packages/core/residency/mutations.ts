import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { residencyKeys } from "./queries";
import { runtimeKeys } from "../runtimes/queries";
import type { DataResidencyPolicy, RuntimeCompliance } from "./schemas";

/**
 * Saving the policy changes which runtimes are eligible, so the runtimes list
 * is refetched with it — the compliance badges on that list are derived from
 * the policy the server just accepted, not from the one the form held.
 */
export function useSaveDataResidencyPolicy(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (policy: DataResidencyPolicy) => api.putDataResidencyPolicy(policy),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: residencyKeys.policy(wsId) });
      qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
    },
  });
}

/** Declare (or re-declare) where one runtime runs. */
export function useDeclareRuntimeCompliance(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ runtimeId, declaration }: { runtimeId: string; declaration: RuntimeCompliance }) =>
      api.putRuntimeCompliance(runtimeId, declaration),
    onSettled: () => qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) }),
  });
}

/** Clear a declaration. Under a restrictive policy the runtime goes ineligible. */
export function useClearRuntimeCompliance(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (runtimeId: string) => api.deleteRuntimeCompliance(runtimeId),
    onSettled: () => qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) }),
  });
}
