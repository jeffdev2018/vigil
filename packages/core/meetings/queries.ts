import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const meetingKeys = {
  all: (wsId: string) => ["meetings", wsId] as const,
  list: (wsId: string) => [...meetingKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) =>
    [...meetingKeys.all(wsId), "detail", id] as const,
};

/** The workspace's meetings, newest first. Transcripts are omitted here. */
export function meetingListOptions(wsId: string) {
  return queryOptions({
    queryKey: meetingKeys.list(wsId),
    queryFn: ({ signal }) => api.listMeetings(undefined, { signal }),
  });
}

/**
 * One meeting with its transcript and action items.
 *
 * Summarizing happens inside the finish request, but another client (or a
 * reload mid-finish) can land on a meeting that is still `summarizing`, and
 * nothing pushes the transition. Poll while that is the state so the summary
 * and its action items appear without a manual refresh.
 */
export function meetingDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: meetingKeys.detail(wsId, id),
    queryFn: ({ signal }) => api.getMeeting(id, { signal }),
    enabled: id.length > 0,
    refetchInterval: (query) =>
      query.state.data?.status === "summarizing" ? 3000 : false,
  });
}
