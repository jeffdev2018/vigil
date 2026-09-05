import type { QueryClient } from "@tanstack/react-query";
import { triageKeys } from "./queries";

/**
 * Refresh the whole triage projection. Both triage:new and triage:resolved can
 * move an item into, out of, or across queue states, and the server owns that
 * split — so invalidate and refetch rather than patching local state. The
 * queue is small (bounded by the pending backlog), so a full refetch is cheap.
 */
export function onTriageInvalidate(qc: QueryClient, wsId: string) {
  qc.invalidateQueries({ queryKey: triageKeys.all(wsId) });
}
