import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";

/**
 * Calendar subscription (ICS). The feed is per user AND per workspace on the
 * server — the URL is a capability, so subscribing in one workspace does not
 * subscribe in another — hence `wsId` in every key.
 */
export const calendarKeys = {
  all: (wsId: string) => ["calendar", wsId] as const,
  feed: (wsId: string) => [...calendarKeys.all(wsId), "feed"] as const,
  upcoming: (wsId: string, within: string) =>
    [...calendarKeys.all(wsId), "upcoming", within] as const,
};

/** The window the meeting prompt asks about: now, or about to start. */
export const CALENDAR_DEFAULT_WITHIN = "30m";

/**
 * How long an upcoming answer is reused. The server caches the download for
 * five minutes, so asking more often than this only costs a round trip; a
 * minute keeps "starting in 15 minutes" honest as it counts down.
 */
const UPCOMING_STALE_MS = 60_000;

export function calendarFeedOptions(wsId: string) {
  return queryOptions({
    queryKey: calendarKeys.feed(wsId),
    queryFn: ({ signal }) => api.getCalendarFeed({ signal }),
    enabled: wsId.length > 0,
  });
}

/**
 * What is running or about to. A user with no feed gets `configured: false`
 * and an empty list, so callers must branch on `configured` rather than
 * reading an empty list as a free afternoon.
 *
 * Not retried: the failure mode is a feed URL that does not work, and three
 * more attempts do not change that — they just delay the message that says so.
 */
export function calendarUpcomingOptions(
  wsId: string,
  within: string = CALENDAR_DEFAULT_WITHIN,
) {
  return queryOptions({
    queryKey: calendarKeys.upcoming(wsId, within),
    queryFn: ({ signal }) => api.calendarUpcoming(within, { signal }),
    enabled: wsId.length > 0,
    staleTime: UPCOMING_STALE_MS,
    retry: false,
  });
}

/**
 * Saves the feed URL. Not optimistic: the server normalizes webcal:// to
 * https:// and clears the previous fetch outcome, so the row that comes back
 * is the one to show.
 */
export function useSetCalendarFeed(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (url: string) => api.setCalendarFeed(url),
    onSuccess: (feed) => {
      qc.setQueryData(calendarKeys.feed(wsId), feed);
      // A new calendar answers a different question.
      qc.invalidateQueries({ queryKey: calendarKeys.all(wsId) });
    },
  });
}

export function useDeleteCalendarFeed(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.deleteCalendarFeed(),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: calendarKeys.all(wsId) });
    },
  });
}
