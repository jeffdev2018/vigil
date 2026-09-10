/**
 * Runs fleet realtime (OS plan, chantier 4). Reference shape is
 * `use-approvals-realtime.ts`: typed `ws.on()`, invalidate-not-patch,
 * one `onReconnect`.
 *
 * Mount point differs from approvals on purpose: approvals mounts
 * listing-level in `_layout.tsx` because the pending-asks feed also backs
 * an always-visible badge elsewhere in the app. Runs has no such badge —
 * the fleet page is the only consumer — so this hook mounts inside
 * `more/runs.tsx` itself and unsubscribes when the user leaves it, same
 * tier as a per-record hook even though it takes no id.
 *
 * `task:*` events invalidate rather than patch: a `Run` row carries fields
 * (`blocked_on`, `duration_ms`, `cost_usd_ticks`) the WS payload doesn't
 * carry and this client cannot recompute (blocker resolution reads
 * approval gates, decisions, goal questions and transition requests
 * server-side) — condition 2 of the patch-over-invalidate rule in
 * apps/mobile/CLAUDE.md. `task:progress` and `task:message` are
 * deliberately NOT subscribed: they fire on every streamed token, and a
 * fleet dashboard doesn't need per-token freshness — the coarser
 * transition events plus pull-to-refresh are enough.
 *
 * `approval:*` events also invalidate: a gate/decision/goal-question being
 * asked or settled changes a run's `blocked_on` even though the run's own
 * status hasn't moved.
 *
 * `run_halt:changed` is NOT in this build's `WSEventType` union
 * (`packages/core/types/events.ts`, out of scope for this change — see the
 * task brief) even though the server already emits it
 * (`server/pkg/protocol/events.go`). Subscribed via `onAny` + a manual
 * type check instead of `ws.on()`, which only accepts known event names.
 */
import { useQueryClient } from "@tanstack/react-query";
import { runsKeys } from "@/data/queries/runs";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";

export function useRunsRealtime() {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      const invalidate = () => {
        qc.invalidateQueries({ queryKey: runsKeys.all(wsId) });
      };
      return [
        ws.on("task:queued", invalidate),
        ws.on("task:dispatch", invalidate),
        ws.on("task:running", invalidate),
        ws.on("task:waiting_local_directory", invalidate),
        ws.on("task:completed", invalidate),
        ws.on("task:failed", invalidate),
        ws.on("task:cancelled", invalidate),
        ws.on("task:scored", invalidate),
        ws.on("task:escalated", invalidate),
        ws.on("approval:asked", invalidate),
        ws.on("approval:decided", invalidate),
        ws.onAny((msg) => {
          if ((msg.type as string) === "run_halt:changed") invalidate();
        }),
        ws.onReconnect(invalidate),
      ];
    },
    [qc],
  );
}
