import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { CreateWorkspaceNoteInput, UpdateWorkspaceNoteInput } from "../types";
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
