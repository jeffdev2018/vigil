import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/**
 * Native calendar (OS plan, chantier 19): events with members and agents as
 * participants, the joined agenda, and the free-slot finder. Distinct from
 * `@multica/core/calendar` (the ICS feed Multica *reads*) and from the
 * outbound feed token below, which is what Multica *publishes*.
 */
export const calendarEventKeys = {
  all: (wsId: string) => ["calendar-events", wsId] as const,
  list: (wsId: string, from: string, to: string) =>
    [...calendarEventKeys.all(wsId), "list", from, to] as const,
  detail: (wsId: string, id: string) =>
    [...calendarEventKeys.all(wsId), "detail", id] as const,
  agenda: (wsId: string, from: string, to: string) =>
    [...calendarEventKeys.all(wsId), "agenda", from, to] as const,
  slots: (
    wsId: string,
    participants: string,
    durationMinutes: number,
    from: string,
    to: string,
    tz: string,
  ) =>
    [...calendarEventKeys.all(wsId), "slots", participants, durationMinutes, from, to, tz] as const,
  feedToken: (wsId: string) => [...calendarEventKeys.all(wsId), "feed-token"] as const,
};

export function calendarEventsOptions(wsId: string, from: string, to: string) {
  return queryOptions({
    queryKey: calendarEventKeys.list(wsId, from, to),
    queryFn: ({ signal }) => api.listCalendarEvents({ from, to }, { signal }),
    enabled: wsId.length > 0 && from.length > 0 && to.length > 0,
  });
}

export function calendarEventDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: calendarEventKeys.detail(wsId, id),
    queryFn: ({ signal }) => api.getCalendarEvent(id, { signal }),
    enabled: wsId.length > 0 && id.length > 0,
  });
}

export function calendarAgendaOptions(wsId: string, from: string, to: string) {
  return queryOptions({
    queryKey: calendarEventKeys.agenda(wsId, from, to),
    queryFn: ({ signal }) => api.getCalendarAgenda({ from, to }, { signal }),
    enabled: wsId.length > 0 && from.length > 0 && to.length > 0,
  });
}

/**
 * Free windows for a set of participants. `participants` is already the
 * wire format (`member:<id>,agent:<id>`) — enabled only once it names at
 * least one, so "Find a slot" before anyone is picked does not fire an
 * endpoint the server would 400 on empty participants anyway.
 */
export function calendarSlotsOptions(
  wsId: string,
  participants: string,
  durationMinutes: number,
  from: string,
  to: string,
  tz: string,
) {
  return queryOptions({
    queryKey: calendarEventKeys.slots(wsId, participants, durationMinutes, from, to, tz),
    queryFn: ({ signal }) =>
      api.findCalendarSlots({ participants, durationMinutes, from, to, tz }, { signal }),
    enabled: wsId.length > 0 && participants.length > 0,
  });
}

/** Whether the caller has minted their outbound ICS feed URL already. */
export function feedTokenOptions(wsId: string) {
  return queryOptions({
    queryKey: calendarEventKeys.feedToken(wsId),
    queryFn: ({ signal }) => api.getCalendarEventFeedToken({ signal }),
    enabled: wsId.length > 0,
  });
}
