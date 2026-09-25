import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { CalendarEventInput } from "../types";
import { calendarEventKeys } from "./queries";

/**
 * None of these are optimistic (CLAUDE.md state rules): a member's create
 * schedules immediately but an agent's is filed as `proposed`, an update can
 * re-invite participants, and a cancel is a status change a subscribed ICS
 * feed has to see — none of that is locally predictable, so every mutation
 * awaits the server and invalidates rather than patching a guess into cache.
 */

function invalidateAll(qc: ReturnType<typeof useQueryClient>, wsId: string) {
  qc.invalidateQueries({ queryKey: calendarEventKeys.all(wsId) });
}

export function useCreateCalendarEvent(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: CalendarEventInput) => api.createCalendarEvent(data),
    onSuccess: () => invalidateAll(qc, wsId),
  });
}

export function useUpdateCalendarEvent(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; data: CalendarEventInput }) =>
      api.updateCalendarEvent(v.id, v.data),
    onSuccess: (event) => {
      qc.setQueryData(calendarEventKeys.detail(wsId, event.id), event);
      invalidateAll(qc, wsId);
    },
  });
}

export function useCancelCalendarEvent(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.cancelCalendarEvent(id),
    onSuccess: () => invalidateAll(qc, wsId),
  });
}

export function useRespondCalendarEvent(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; response: "accepted" | "declined" | "tentative" }) =>
      api.respondCalendarEvent(v.id, v.response),
    onSuccess: (event) => {
      qc.setQueryData(calendarEventKeys.detail(wsId, event.id), event);
      invalidateAll(qc, wsId);
    },
  });
}

/**
 * Mints (or rotates) the caller's outbound ICS feed URL. The clear URL is in
 * this response only — the caller shows it once, same pattern as the triage
 * email source and the runtime pairing token.
 */
export function useMintCalendarFeedToken(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.mintCalendarEventFeedToken(),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: calendarEventKeys.feedToken(wsId) });
    },
  });
}

export function useRevokeCalendarFeedToken(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.revokeCalendarEventFeedToken(),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: calendarEventKeys.feedToken(wsId) });
    },
  });
}

export function useImportGoogleCalendar(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { from?: string; to?: string }) => api.importGoogleCalendar(v),
    onSuccess: () => invalidateAll(qc, wsId),
  });
}
