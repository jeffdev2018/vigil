/**
 * Native calendar mutations (OS plan, chantier 19). No optimistic patches:
 * the outcome isn't locally predictable (accept/decline/cancel all fan out
 * server-side — a decline cancels the event, a moved/created event notifies
 * other participants), so every mutation here awaits the server and
 * invalidates on settle, matching root CLAUDE.md's optimistic-update gate.
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/data/api";
import { calendarKeys } from "@/data/queries/calendar";
import { inboxKeys } from "@/data/queries/inbox";
import { useWorkspaceStore } from "@/data/workspace-store";
import type { CalendarEventInput } from "@multica/core/types";

export function useRespondCalendarEvent() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return useMutation({
    mutationFn: (v: { id: string; response: "accepted" | "declined" | "tentative" }) =>
      api.respondCalendarEvent(v.id, v.response),
    onSettled: (_data, _err, v) => {
      qc.invalidateQueries({ queryKey: calendarKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: calendarKeys.event(wsId, v.id) });
      // A reminder/invitation row's affordance can go stale once answered.
      qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
    },
  });
}

export function useCancelCalendarEvent() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return useMutation({
    mutationFn: (id: string) => api.cancelCalendarEvent(id),
    onSettled: (_data, _err, id) => {
      qc.invalidateQueries({ queryKey: calendarKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: calendarKeys.event(wsId, id) });
    },
  });
}

export function useCreateCalendarEvent() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return useMutation({
    mutationFn: (body: CalendarEventInput) => api.createCalendarEvent(body),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: calendarKeys.all(wsId) });
    },
  });
}
