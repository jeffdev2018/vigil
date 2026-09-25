import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { doctrineKeys } from "./queries";
import type { DoctrinePublishInput } from "./schemas";

/**
 * None of these are optimistic (CLAUDE.md state rules). Publishing may land
 * as a live revision or be held for a second reviewer depending on a policy
 * and on who else can review — the outcome is not locally predictable — and
 * approve / reject / restore move the live document for everyone. Each one
 * awaits the server and invalidates on settle.
 */
function invalidateAll(qc: ReturnType<typeof useQueryClient>, wsId: string) {
  qc.invalidateQueries({ queryKey: doctrineKeys.all(wsId) });
}

export function usePublishDoctrine(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: DoctrinePublishInput) => api.publishWorkspaceDoctrine(input),
    onSettled: () => invalidateAll(qc, wsId),
  });
}

export function useApproveDoctrineVersion(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; note?: string }) => api.approveDoctrineVersion(v.id, v.note),
    onSettled: () => invalidateAll(qc, wsId),
  });
}

export function useRejectDoctrineVersion(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; note?: string }) => api.rejectDoctrineVersion(v.id, v.note),
    onSettled: () => invalidateAll(qc, wsId),
  });
}

/** Republishes an earlier revision's text through the same review policy. */
export function useRestoreDoctrineVersion(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; expected_revision: number; note?: string }) =>
      api.restoreDoctrineVersion(v.id, v.expected_revision, v.note),
    onSettled: () => invalidateAll(qc, wsId),
  });
}

export function useAcknowledgeDoctrineReport(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; note?: string }) => api.acknowledgeDoctrineReport(v.id, v.note),
    onSettled: () => invalidateAll(qc, wsId),
  });
}

export function useDismissDoctrineReport(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; note?: string }) => api.dismissDoctrineReport(v.id, v.note),
    onSettled: () => invalidateAll(qc, wsId),
  });
}
