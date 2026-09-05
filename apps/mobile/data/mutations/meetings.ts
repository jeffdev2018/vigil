/**
 * Meeting rename / delete / re-summarize.
 *
 * Mirrors web `packages/core/meetings/mutations.ts`, including the cross-
 * feature effect: re-summarizing re-extracts the action items, and every
 * action item is a triage entry — so it must invalidate the triage caches or
 * the Triage screen keeps showing the previous run's items.
 *
 * All three await the server (they navigate, confirm, or replace a whole
 * record), so none is optimistic. Rename and re-summarize write the returned
 * meeting into the detail cache before invalidating the list, so the open
 * screen updates in the same frame.
 *
 * Recording mutations (create / append segment / finish) are absent on
 * purpose: mobile does not record audio.
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { Meeting } from "@multica/core/types";
import { api } from "@/data/api";
import { meetingKeys } from "@/data/queries/meetings";
import { triageKeys } from "@/data/queries/triage";
import { useWorkspaceStore } from "@/data/workspace-store";

export function useRenameMeeting() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation<Meeting, Error, { id: string; title: string }>({
    mutationFn: ({ id, title }) => api.updateMeeting(id, { title }),
    onSuccess: (meeting, { id }) => {
      if (!wsId) return;
      qc.setQueryData(meetingKeys.detail(wsId, id), meeting);
      qc.invalidateQueries({ queryKey: meetingKeys.list(wsId) });
    },
  });
}

export function useDeleteMeeting() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation<void, Error, string>({
    mutationFn: (id) => api.deleteMeeting(id),
    onSuccess: (_res, id) => {
      if (!wsId) return;
      // Remove rather than invalidate: the record is gone, and a refetch
      // would only 404. The caller navigates away in the same handler.
      qc.removeQueries({ queryKey: meetingKeys.detail(wsId, id) });
      qc.invalidateQueries({ queryKey: meetingKeys.list(wsId) });
    },
  });
}

export function useResummarizeMeeting() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation<Meeting, Error, string>({
    mutationFn: (id) => api.resummarizeMeeting(id),
    onSuccess: (meeting, id) => {
      if (!wsId) return;
      qc.setQueryData(meetingKeys.detail(wsId, id), meeting);
    },
    onSettled: (_res, _err, id) => {
      if (!wsId) return;
      qc.invalidateQueries({ queryKey: meetingKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: meetingKeys.detail(wsId, id) });
      // Action items are triage entries: a new extraction replaces them.
      qc.invalidateQueries({ queryKey: triageKeys.all(wsId) });
    },
  });
}
