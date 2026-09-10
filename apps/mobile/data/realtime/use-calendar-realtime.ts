/**
 * Native calendar realtime (OS plan, chantier 19). `calendar:changed` fires
 * on create / update / status change / respond
 * (server/internal/handler/calendar_events.go publishCalendarChanged).
 *
 * Invalidate-not-patch: the payload is just
 * `{event_id, issue_id, status, starts_at}` — not the full CalendarEventEntry
 * (condition 1 of the patch-over-invalidate rule in apps/mobile/CLAUDE.md),
 * and the Agenda screen's window-keyed cache can't locate which window(s)
 * the event belongs to without re-deriving the day-key logic here. Also
 * invalidates the inbox list: an accepted/declined/cancelled event changes
 * what a `calendar_invitation` / `calendar_reminder` row can still offer.
 *
 * Mounted listing-level in `_layout.tsx` (not per-screen) because both the
 * Agenda screen and any open inbox notification detail care, and there's
 * no per-record id to scope a per-record mount to (an agenda window has no
 * stable "record").
 */
import { useQueryClient } from "@tanstack/react-query";
import { calendarKeys } from "@/data/queries/calendar";
import { inboxKeys } from "@/data/queries/inbox";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";

export function useCalendarRealtime() {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      const invalidate = () => {
        qc.invalidateQueries({ queryKey: calendarKeys.all(wsId) });
        qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
      };
      return [
        ws.on("calendar:changed", invalidate),
        // The agenda also carries wake-ups (JEF-373), so a follow-up
        // scheduled or cancelled anywhere moves a window of this cache.
        ws.on("followup:changed", () => {
          qc.invalidateQueries({ queryKey: calendarKeys.all(wsId) });
        }),
        ws.onReconnect(invalidate),
      ];
    },
    [qc],
  );
}
