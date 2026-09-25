/**
 * Workspace doctrine realtime (OS plan, chantier 22). `doctrine:changed`
 * fires when a revision is published, proposed, approved, rejected, or when
 * a report is filed or resolved (server/internal/handler/
 * workspace_doctrine.go, every `h.publish(protocol.EventDoctrineChanged…)`
 * call).
 *
 * Invalidate-not-patch: the payload is a change hint
 * (`{revision, version_id | report_id, change}`) rather than the row —
 * condition 1 of the patch-over-invalidate rule in apps/mobile/CLAUDE.md —
 * and one event can move the live doctrine, the pending proposal, the open
 * report count and the report lists at once. `doctrineKeys.all` covers all
 * of them, including every cached diff (an approved proposal changes what
 * "compared with the live doctrine" means).
 *
 * The inbox list is invalidated too: a `doctrine_review` /
 * `doctrine_report` notification stops being actionable once the version or
 * report it points at is resolved — same cross-feature reasoning as
 * `use-calendar-realtime.ts`.
 *
 * Mounted listing-level in `_layout.tsx` (not per-screen) because the
 * open-reports badge in the More popover must stay fresh while the user is
 * anywhere in the workspace, and there is no per-record id to scope to.
 */
import { useQueryClient } from "@tanstack/react-query";
import { doctrineKeys } from "@/data/queries/doctrine";
import { inboxKeys } from "@/data/queries/inbox";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";

export function useDoctrineRealtime() {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      const invalidate = () => {
        qc.invalidateQueries({ queryKey: doctrineKeys.all(wsId) });
        qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
      };
      return [
        ws.on("doctrine:changed", invalidate),
        ws.onReconnect(invalidate),
      ];
    },
    [qc],
  );
}
