import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { AcceptTriageItemOverrides, TriageSourcePatch } from "../types";
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
    mutationFn: (v: string | { itemId: string; overrides?: AcceptTriageItemOverrides }) =>
      typeof v === "string"
        ? api.acceptTriageItem(v)
        : api.acceptTriageItem(v.itemId, v.overrides),
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

// A source's policy only changes what future deliveries do, so only the stats
// (which carry each source's policy and counters) need a refresh.
export function useUpdateTriageSourceSettings(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { sourceId: string; patch: TriageSourcePatch }) =>
      api.updateTriageSourceSettings(v.sourceId, v.patch),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: triageKeys.stats(wsId) });
    },
  });
}

/** Triage auto-ML (K61): a dismissed item (human, rule or auto) goes back to pending. */
export function useReopenTriageItem(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (itemId: string) => api.reopenTriageItem(itemId),
    onSettled: () => qc.invalidateQueries({ queryKey: triageKeys.all(wsId) }),
  });
}

/**
 * Merge folds an item into an issue that already tracks the work: no second
 * issue, and the target gets a system comment. The issue list can change
 * (a comment lands on it), so both projections are invalidated.
 */
export function useMergeTriageItem(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { itemId: string; issueId: string }) =>
      api.mergeTriageItem(v.itemId, v.issueId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: triageKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

/** Snooze parks a pending item until `until` (ISO). Nothing is resolved. */
export function useSnoozeTriageItem(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { itemId: string; until: string }) =>
      api.snoozeTriageItem(v.itemId, v.until),
    onSettled: () => qc.invalidateQueries({ queryKey: triageKeys.all(wsId) }),
  });
}

/** Batch dismiss: one shared reason, per-item outcomes in the response. */
export function useBatchDismissTriageItems(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { itemIds: string[]; reason?: string }) =>
      api.batchDismissTriageItems(v.itemIds, v.reason),
    onSettled: () => qc.invalidateQueries({ queryKey: triageKeys.all(wsId) }),
  });
}
