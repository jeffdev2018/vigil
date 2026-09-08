import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { repoIndexKeys } from "./queries";
import type { RepoIndexSettingsInput } from "./schemas";

/**
 * Flip one repository's opt-in.
 *
 * Not optimistic: turning the index OFF also purges that repository's stored
 * chunks, so the counts the row displays change as a consequence of the write.
 * Predicting them locally would show a user "0 chunks" for a save that failed,
 * or leave a stale count after one that succeeded.
 */
export function useSaveRepoIndexSettings(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: RepoIndexSettingsInput) => api.putRepoIndexSettings(input),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: repoIndexKeys.settings(wsId) });
    },
  });
}
