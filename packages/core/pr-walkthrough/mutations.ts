import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { prWalkthroughKeys } from "./queries";
import type { PrWalkthroughSettings } from "./schemas";

// Refresh starts a run whose result lands later, so nothing here is
// optimistic: the query is invalidated and the row comes back `pending`.

export function useRefreshPrWalkthrough(wsId: string, issueId: string, prId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.refreshPrWalkthrough(issueId, prId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: prWalkthroughKeys.detail(wsId, issueId, prId) });
    },
  });
}

export function useSavePrWalkthroughSettings(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: PrWalkthroughSettings) => api.putPrWalkthroughSettings(input),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: prWalkthroughKeys.settings(wsId) });
    },
  });
}
