/**
 * Packs realtime (OS plan, vague B). `pack:changed` fires when a pack is
 * installed, updated or removed anywhere in the workspace
 * (`server/internal/handler/packs.go`).
 *
 * Invalidate-not-patch: the payload is a change hint
 * (`{pack_id, version, change}`) rather than the rows — condition 1 of the
 * patch-over-invalidate rule in apps/mobile/CLAUDE.md — and one install can
 * create statuses, labels, agents, projects, goals, an org chart, work items
 * and a doctrine section in a single transaction. `invalidatePackTargets`
 * covers exactly the same caches web's `pack:changed` listener does
 * (`packages/core/realtime/use-realtime-sync.ts`), minus the two mobile
 * doesn't have (skills, autopilots).
 *
 * Mounted listing-level in `_layout.tsx` because the update badge in the
 * More popover must stay fresh from anywhere in the workspace, and there is
 * no per-record id to scope to.
 */
import { useQueryClient } from "@tanstack/react-query";
import { invalidatePackTargets } from "@/data/mutations/packs";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";

export function usePacksRealtime() {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      const invalidate = () => invalidatePackTargets(qc, wsId);
      return [ws.on("pack:changed", invalidate), ws.onReconnect(invalidate)];
    },
    [qc],
  );
}
