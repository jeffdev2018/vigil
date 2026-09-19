/**
 * Approvals realtime — listing-level (Layer 3), always-on for the workspace
 * session. `approval:asked` / `approval:decided` fire whenever the pending-
 * asks feed (GET /api/approvals) changes: a new Decision Card, held
 * transition or goal question, or one of those being answered.
 *
 * Typed `ws.on()` like every other realtime hook here: the events are
 * declared in `packages/core/types/events.ts` (WSEventType) and emitted by
 * server/pkg/protocol/events.go EventApprovalAsked / EventApprovalDecided.
 *
 * Payload shape is `{source, id, issue_id, kind, outcome?}` — unused here,
 * every handler just invalidates and lets the refetch pick up the new
 * state, same as `useInboxRealtime`'s `inbox:*` handlers.
 *
 * Both the workspace-level feed (`approvalKeys.workspace`) and any open
 * issue's feed (`approvalKeys.issue`) live under `approvalKeys.all(wsId)`,
 * so one invalidate covers both — no per-record mount needed even though
 * the timeline reads a per-issue query.
 */
import { useQueryClient } from "@tanstack/react-query";
import { approvalKeys } from "@/data/queries/approvals";
import { inboxKeys } from "@/data/queries/inbox";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";

export function useApprovalsRealtime() {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      const invalidate = () => {
        qc.invalidateQueries({ queryKey: approvalKeys.all(wsId) });
        // A new/decided ask also changes what the inbox shows
        // (decision_request, transition_approval_requested, goal_question
        // rows), mirroring the cross-cutting pattern in
        // use-inbox-realtime.ts.
        qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
      };
      return [ws.on("approval:asked", invalidate), ws.on("approval:decided", invalidate), ws.onReconnect(invalidate)];
    },
    [qc],
  );
}
