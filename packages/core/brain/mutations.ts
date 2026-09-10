import { useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { getShortcutRuntime } from "../shortcuts/platform";
import type {
  BrainCaptureOrigin,
  CreateBrainCaptureInput,
  CreateWorkspaceNoteInput,
  OrganizeBrainCaptureInput,
  UpdateWorkspaceNoteInput,
  UploadBrainCaptureInput,
} from "../types";
import { brainKeys } from "./queries";

/**
 * None of these are optimistic. A Brain write changes `revision`, and the
 * server owns it: guessing the next revision locally would hand the next PATCH
 * a token the server never issued, turning the conflict guard into a lie.
 * CLAUDE.md rule: optimistic only when the outcome is locally predictable.
 */
export function useCreateWorkspaceNote(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateWorkspaceNoteInput) => api.createWorkspaceNote(input),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: brainKeys.all(wsId) });
    },
  });
}

export function useUpdateWorkspaceNote(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: UpdateWorkspaceNoteInput }) =>
      api.updateWorkspaceNote(id, input),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: brainKeys.all(wsId) });
    },
  });
}

export function useSetWorkspaceNoteArchived(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, archived }: { id: string; archived: boolean }) =>
      api.setWorkspaceNoteArchived(id, archived),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: brainKeys.all(wsId) });
    },
  });
}

export function useDeleteWorkspaceNote(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteWorkspaceNote(id),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: brainKeys.all(wsId) });
    },
  });
}

/**
 * Brain capture inbox. Nothing here is optimistic either: a capture's kind,
 * its suggestion and (for a voice memo) its transcript are all decided
 * server-side, and organizing one moves it out of the inbox and may create or
 * append a note. Every mutation awaits the server, then invalidates.
 */

/** Where this client is running, in the `origin` vocabulary the server takes. */
export function captureOrigin(): BrainCaptureOrigin {
  return getShortcutRuntime() === "desktop" ? "desktop" : "web";
}

/**
 * A capture write can create or change a note (organize/merge), so it
 * invalidates the whole Brain prefix — captures, notes and search hits all
 * live under it.
 */
function invalidateBrain(qc: QueryClient, wsId: string): void {
  qc.invalidateQueries({ queryKey: brainKeys.all(wsId) });
}

export function useCaptureText(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateBrainCaptureInput) =>
      api.createBrainCapture({ origin: captureOrigin(), ...input }),
    onSettled: () => invalidateBrain(qc, wsId),
  });
}

export function useCaptureUpload(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: UploadBrainCaptureInput) =>
      api.uploadBrainCapture({ origin: captureOrigin(), ...input }),
    onSettled: () => invalidateBrain(qc, wsId),
  });
}

export function useSuggestCapture(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.suggestBrainCapture(id),
    onSettled: () => invalidateBrain(qc, wsId),
  });
}

export function useOrganizeCapture(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: OrganizeBrainCaptureInput }) =>
      api.organizeBrainCapture(id, input),
    onSettled: () => invalidateBrain(qc, wsId),
  });
}

export function useReopenCapture(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.reopenBrainCapture(id),
    onSettled: () => invalidateBrain(qc, wsId),
  });
}

/** Destructive and irreversible: the row goes, and its file with it unless a
 *  note holds the attachment. Confirmed in the UI, never optimistic. */
export function useDeleteCapture(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteBrainCapture(id),
    onSettled: () => invalidateBrain(qc, wsId),
  });
}
