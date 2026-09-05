/**
 * Meeting cache key factory + query options.
 *
 * Key shape mirrors web `packages/core/meetings/queries.ts` —
 * `["meetings", wsId, "list"]` / `["meetings", wsId, "detail", id]`.
 *
 * `GET /api/meetings` is the one paginated endpoint in this parity batch that
 * has no cursor: pages are offsets. A meeting created between two fetches
 * shifts the window by one, which for an append-only "load older" list costs
 * at worst a duplicate row — same trade web accepts.
 *
 * There are no meeting WebSocket events (checked: nothing with a `meeting:`
 * prefix in packages/core/types/events.ts or server/pkg/protocol/events.go),
 * so freshness on the detail screen comes from a bounded poll instead — see
 * `isMeetingSummaryStalled`.
 */
import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import type { Meeting } from "@multica/core/types";
import { api } from "@/data/api";

/** One page of meetings. The server caps a page at 100; this is its default. */
export const MEETINGS_PAGE_SIZE = 50;

/** How long a `summarizing` meeting may run before it counts as stuck. */
export const MEETING_SUMMARY_STALL_MS = 5 * 60_000;

const MEETING_SUMMARY_POLL_MS = 3000;

export const meetingKeys = {
  all: (wsId: string | null) => ["meetings", wsId] as const,
  list: (wsId: string | null) => [...meetingKeys.all(wsId), "list"] as const,
  detail: (wsId: string | null, id: string) =>
    [...meetingKeys.all(wsId), "detail", id] as const,
};

export const meetingListOptions = (wsId: string | null) =>
  infiniteQueryOptions({
    queryKey: meetingKeys.list(wsId),
    initialPageParam: 0,
    queryFn: ({ pageParam, signal }) =>
      api.listMeetings(
        { limit: MEETINGS_PAGE_SIZE, offset: pageParam },
        { signal },
      ),
    // A short page is the last one: the endpoint reports no total.
    getNextPageParam: (page, allPages) =>
      page.meetings.length < MEETINGS_PAGE_SIZE
        ? undefined
        : allPages.reduce((n, p) => n + p.meetings.length, 0),
    enabled: !!wsId,
  });

/**
 * True when a meeting has been `summarizing` for longer than any summary
 * takes. Mirrors `isMeetingSummaryStalled` in packages/core/meetings/
 * queries.ts, itself mirroring `meetingSummaryStale` in
 * server/internal/handler/meeting.go.
 *
 * Summarizing happens INSIDE the finish request: if that request dies, nothing
 * moves the row out of `summarizing` and a naive client polls it forever.
 * `ended_at` is stamped by the statement that enters the state, so it dates
 * the attempt. No usable timestamp → keep polling rather than declare a
 * healthy run stuck.
 */
export function isMeetingSummaryStalled(
  meeting: { status: string; ended_at?: string } | undefined,
  now: number = Date.now(),
): boolean {
  if (meeting?.status !== "summarizing") return false;
  const startedAt = meeting.ended_at ? Date.parse(meeting.ended_at) : NaN;
  if (Number.isNaN(startedAt)) return false;
  return now - startedAt >= MEETING_SUMMARY_STALL_MS;
}

/**
 * One meeting with its transcript and action items. Polls only while the
 * summary is actually being written, and stops once the attempt is old enough
 * to be dead — on cellular, polling a corpse every 3s is a real cost.
 */
export const meetingDetailOptions = (wsId: string | null, id: string) =>
  queryOptions({
    queryKey: meetingKeys.detail(wsId, id),
    queryFn: ({ signal }) => api.getMeeting(id, { signal }),
    enabled: !!wsId && id.length > 0,
    refetchInterval: (query) => {
      const meeting = query.state.data;
      if (meeting?.status !== "summarizing") return false;
      return isMeetingSummaryStalled(meeting) ? false : MEETING_SUMMARY_POLL_MS;
    },
  });

export function flattenMeetingPages(
  pages: { meetings: Meeting[] }[] | undefined,
): Meeting[] {
  return (pages ?? []).flatMap((page) => page.meetings);
}
