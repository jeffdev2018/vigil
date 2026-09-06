import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { docDriftKeys } from "./queries";
import type { DocDriftSettingsInput } from "./schemas";

// Every mutation here changes what a proposal means, so both caches are
// refetched on settle rather than patched: a check starts a run whose result
// arrives later, and dismiss / open-pr change a row the settings surface also
// summarises.

function invalidate(qc: ReturnType<typeof useQueryClient>, wsId: string) {
  qc.invalidateQueries({ queryKey: docDriftKeys.settings(wsId) });
  qc.invalidateQueries({ queryKey: docDriftKeys.proposals(wsId) });
}

export function useSaveDocDriftSettings(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: DocDriftSettingsInput) => api.putDocDriftSettings(input),
    onSettled: () => invalidate(qc, wsId),
  });
}

/** Force a check for one repository now, whatever its last checked commit. */
export function useCheckDocDrift(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (repoIdentifier: string) => api.checkDocDrift(repoIdentifier),
    onSettled: () => invalidate(qc, wsId),
  });
}

export function useDismissDocDriftProposal(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.dismissDocDriftProposal(id),
    onSettled: () => invalidate(qc, wsId),
  });
}

export function useOpenDocDriftProposalPR(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.openDocDriftProposalPR(id),
    onSettled: () => invalidate(qc, wsId),
  });
}
