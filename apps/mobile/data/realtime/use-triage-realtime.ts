/**
 * Triage + postmortem realtime — listing-level.
 *
 * Both features have a counter on an always-mounted surface (the Triage and
 * Postmortems rows in the More popover carry pending/draft badges), so they
 * have a consumer even when their screen is not open — which is what makes
 * mounting these globally worth the CPU, per apps/mobile/CLAUDE.md
 * ("If a new event has no consumer on mobile, don't subscribe").
 *
 * Invalidate rather than patch, for the reasons the cellular-data rule allows:
 *   - `triage:new` / `triage:resolved` carry only `{item_id, source_id?}` /
 *     `{item_id, state?}` — not the row, so there is nothing to patch in.
 *   - `postmortem:created` / `postmortem:resolved` DO carry the full object,
 *     but the caches are per-state infinite pages: a resolve moves the item
 *     between two different query keys at a page boundary we cannot predict.
 *
 * That is also what web does — `packages/core/realtime/use-realtime-sync.ts`
 * dispatches on the event prefix and calls one debounced full invalidate per
 * family (`onTriageInvalidate` / `onPostmortemInvalidate`). One handler per
 * family here mirrors that; four per-event handlers would just do the same
 * work four times.
 */
import { useQueryClient } from "@tanstack/react-query";
import { triageKeys } from "@/data/queries/triage";
import { postmortemKeys } from "@/data/queries/postmortem";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";

export function useTriageRealtime() {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      // Prefix invalidate: every per-state list plus the stats badge.
      const triage = () =>
        qc.invalidateQueries({ queryKey: triageKeys.all(wsId) });
      const postmortem = () =>
        qc.invalidateQueries({ queryKey: postmortemKeys.all(wsId) });

      return [
        ws.on("triage:new", triage),
        ws.on("triage:resolved", triage),
        ws.on("postmortem:created", postmortem),
        ws.on("postmortem:resolved", postmortem),
        ws.onReconnect(() => {
          triage();
          postmortem();
        }),
      ];
    },
    [qc],
  );
}
