import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { triageKeys } from "../triage/queries";
import { meetingKeys } from "./queries";

/**
 * None of these are optimistic. Creating a meeting is a flow that opens a
 * recorder against a server-assigned id, and finishing one summarizes on the
 * server and queues triage items — neither outcome is locally predictable, so
 * the caller awaits the server (CLAUDE.md state rules).
 */
export function useCreateMeeting(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v?: { title?: string; appName?: string }) =>
      api.createMeeting({ title: v?.title, app_name: v?.appName }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: meetingKeys.list(wsId) });
    },
  });
}

/**
 * One transcribed audio chunk. The recorder uploads these strictly
 * sequentially, so this mutation is driven with `mutateAsync` from its own
 * queue rather than fired per chunk.
 */
export function useAppendMeetingTextSegment() {
  return useMutation({
    mutationFn: (v: { meetingId: string; text: string; seq: number }) =>
      api.appendMeetingTextSegment(v.meetingId, v.text, v.seq),
  });
}

export function useAppendMeetingSegment() {
  return useMutation({
    mutationFn: (v: { meetingId: string; chunk: Blob; seq: number }) =>
      api.appendMeetingSegment(v.meetingId, v.chunk, v.seq),
  });
}

/**
 * Renames a meeting. Not optimistic either: the server trims and truncates the
 * title, so the value that comes back is the one to show.
 */
export function useRenameMeeting(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { meetingId: string; title: string }) =>
      api.updateMeeting(v.meetingId, { title: v.title }),
    onSuccess: (meeting) => {
      qc.setQueryData(meetingKeys.detail(wsId, meeting.id), meeting);
      qc.invalidateQueries({ queryKey: meetingKeys.list(wsId) });
    },
  });
}

/**
 * Corrects one transcript paragraph. Not optimistic: the server collapses
 * newlines and truncates, and it answers with the whole meeting, so the
 * response is what the cache should hold. The summary is NOT regenerated —
 * the detail page tells the user to press the existing button.
 */
export function useEditMeetingSegment(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { meetingId: string; seq: number; text: string }) =>
      api.updateMeetingSegment(v.meetingId, v.seq, v.text),
    onSuccess: (meeting) => {
      qc.setQueryData(meetingKeys.detail(wsId, meeting.id), meeting);
    },
  });
}

/**
 * Removes a meeting. The detail page navigates back to the list on success, so
 * the caller awaits the server and nothing is dropped from cache optimistically
 * (CLAUDE.md state rules).
 */
export function useDeleteMeeting(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (meetingId: string) => api.deleteMeeting(meetingId),
    onSuccess: (_data, meetingId) => {
      qc.removeQueries({ queryKey: meetingKeys.detail(wsId, meetingId) });
      qc.invalidateQueries({ queryKey: meetingKeys.list(wsId) });
    },
  });
}

/**
 * Closes the recording and summarizes it. The summary creates pending triage
 * items, so the triage queue is invalidated too.
 */
export function useFinishMeeting(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (meetingId: string) => api.finishMeeting(meetingId),
    onSettled: (_data, _err, meetingId) => {
      qc.invalidateQueries({ queryKey: meetingKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: meetingKeys.detail(wsId, meetingId) });
      qc.invalidateQueries({ queryKey: triageKeys.all(wsId) });
    },
  });
}

/**
 * Re-runs the summary for a meeting that already stopped recording. Like
 * finish, it can queue triage items, so the queue is invalidated too.
 */
export function useResummarizeMeeting(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (meetingId: string) => api.resummarizeMeeting(meetingId),
    onSuccess: (meeting) => {
      qc.setQueryData(meetingKeys.detail(wsId, meeting.id), meeting);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: meetingKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: triageKeys.all(wsId) });
    },
  });
}
