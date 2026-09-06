import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { codeHealthKeys } from "./queries";
import type { CodeHealthSettingsInput } from "./schemas";

/**
 * Saving the settings can also change what the history means (a different
 * agent, a different scope), so both caches are refetched.
 */
export function useSaveCodeHealthSettings(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CodeHealthSettingsInput) => api.putCodeHealthSettings(input),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: codeHealthKeys.settings(wsId) });
      qc.invalidateQueries({ queryKey: codeHealthKeys.scans(wsId) });
    },
  });
}

/** A manual scan appends to the history and starts the poll. */
export function useTriggerCodeHealthScan(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.triggerCodeHealthScan(),
    onSettled: () => qc.invalidateQueries({ queryKey: codeHealthKeys.scans(wsId) }),
  });
}
