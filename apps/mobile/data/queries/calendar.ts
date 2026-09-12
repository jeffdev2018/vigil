import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

/**
 * Calendar cache key factory (OS plan, chantier 19). Three-segment shape
 * mirrors the rest of mobile's data layer (see apps/mobile/CLAUDE.md
 * "Query / mutation factory pattern"). `agenda` is additionally keyed by
 * the window (`from`/`to`) since the Agenda screen pages by fortnight and
 * each window is its own cache entry; `.all` still lets a workspace switch
 * or a `calendar:changed` event invalidate every window + every detail at
 * once.
 */
export const calendarKeys = {
  all: (wsId: string | null) => ["calendar", wsId] as const,
  agenda: (wsId: string | null, from: string, to: string) =>
    [...calendarKeys.all(wsId), "agenda", from, to] as const,
  event: (wsId: string | null, id: string | null) =>
    [...calendarKeys.all(wsId), "event", id] as const,
};

export const calendarAgendaOptions = (
  wsId: string | null,
  from: string,
  to: string,
) =>
  queryOptions({
    queryKey: calendarKeys.agenda(wsId, from, to),
    queryFn: ({ signal }) => api.getCalendarAgenda(from, to, { signal }),
    enabled: !!wsId,
  });

export const calendarEventOptions = (wsId: string | null, id: string | null) =>
  queryOptions({
    queryKey: calendarKeys.event(wsId, id),
    queryFn: ({ signal }) => api.getCalendarEvent(id!, { signal }),
    enabled: !!wsId && !!id,
  });
