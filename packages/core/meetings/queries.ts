import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const meetingKeys = {
  all: (wsId: string) => ["meetings", wsId] as const,
  list: (wsId: string) => [...meetingKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) =>
    [...meetingKeys.all(wsId), "detail", id] as const,
};

/** One page of meetings. The server caps a page at 100; this is its default. */
export const MEETINGS_PAGE_SIZE = 50;

/**
 * The workspace's meetings, newest first, a page at a time. Transcripts are
 * omitted here — the detail endpoint carries them.
 *
 * Paged rather than a flat list: the endpoint has always answered with at most
 * one page, so a workspace past its first 50 meetings simply could not see the
 * older ones. There is no cursor on this endpoint, so pages are offsets; a
 * meeting created between two fetches shifts the window by one, which for an
 * append-only "load older" list is a duplicate row at worst.
 */
export function meetingListOptions(wsId: string) {
  return infiniteQueryOptions({
    queryKey: meetingKeys.list(wsId),
    initialPageParam: 0,
    queryFn: ({ pageParam, signal }) =>
      api.listMeetings({ limit: MEETINGS_PAGE_SIZE, offset: pageParam }, { signal }),
    // A short page is the last one: the endpoint reports no total.
    getNextPageParam: (page, allPages) =>
      page.meetings.length < MEETINGS_PAGE_SIZE
        ? undefined
        : allPages.reduce((n, p) => n + p.meetings.length, 0),
  });
}

/** How long a `summarizing` meeting may run before it counts as stuck. */
export const MEETING_SUMMARY_STALL_MS = 5 * 60_000;

const MEETING_SUMMARY_POLL_MS = 3000;

/**
 * True when a meeting has been `summarizing` for longer than any summary takes.
 *
 * Summarizing happens INSIDE the finish request: if that request dies (the tab
 * was closed, the server restarted), nothing ever moves the row out of
 * `summarizing` and the client would poll it forever. `ended_at` is stamped by
 * the same statement that enters the state, so it dates the attempt.
 *
 * Mirrors meetingSummaryStale in server/internal/handler/meeting.go, which is
 * how long the server makes a re-summarize wait before taking one over.
 */
export function isMeetingSummaryStalled(
  meeting: { status: string; ended_at?: string } | undefined,
  now: number = Date.now(),
): boolean {
  if (meeting?.status !== "summarizing") return false;
  const startedAt = meeting.ended_at ? Date.parse(meeting.ended_at) : NaN;
  // No usable timestamp: keep polling rather than declare a healthy run stuck.
  if (Number.isNaN(startedAt)) return false;
  return now - startedAt >= MEETING_SUMMARY_STALL_MS;
}

/**
 * One meeting with its transcript and action items.
 *
 * Another client (or a reload mid-finish) can land on a meeting that is still
 * `summarizing`, and nothing pushes the transition. Poll while that is the
 * state so the summary and its action items appear without a manual refresh —
 * but stop once the attempt is old enough to be dead, or the tab polls a
 * corpse every 3 seconds for as long as it stays open.
 */
export function meetingDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: meetingKeys.detail(wsId, id),
    queryFn: ({ signal }) => api.getMeeting(id, { signal }),
    enabled: id.length > 0,
    refetchInterval: (query) => {
      const meeting = query.state.data;
      if (meeting?.status !== "summarizing") return false;
      return isMeetingSummaryStalled(meeting) ? false : MEETING_SUMMARY_POLL_MS;
    },
  });
}
