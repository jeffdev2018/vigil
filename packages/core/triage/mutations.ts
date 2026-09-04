import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { TriageSourceMode } from "../types";
import { issueKeys } from "../issues/queries";
import { triageKeys } from "./queries";

/**
 * Accept, dismiss, and batch-accept are deliberately NOT optimistic: each one
 * transitions an item out of the queue (and accept creates a real issue), so
 * the caller awaits the server and we invalidate after settle. CLAUDE.md rule:
 * flows that change state or navigate await the server before proceeding.
 */
export function useAcceptTriageItem(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (itemId: string) => api.acceptTriageItem(itemId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: triageKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

export function useDismissTriageItem(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { itemId: string; reason?: string }) =>
      api.dismissTriageItem(v.itemId, { reason: v.reason }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: triageKeys.all(wsId) });
    },
  });
}

export function useBatchAcceptTriageItems(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (ids: string[]) => api.batchAcceptTriageItems(ids),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: triageKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

// Flipping a source's mode only changes future routing, so only the stats
// (which carry each source's current mode) need a refresh.
export function useUpdateTriageSourceMode(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { sourceId: string; mode: TriageSourceMode }) =>
      api.updateTriageSourceMode(v.sourceId, v.mode),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: triageKeys.stats(wsId) });
    },
  });
}
