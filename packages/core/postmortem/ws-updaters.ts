import type { QueryClient } from "@tanstack/react-query";
import { postmortemKeys } from "./queries";

/**
 * Refresh the whole postmortem projection. Both postmortem:created and
 * postmortem:resolved can move an item into, out of, or across states, and
 * the server owns that split — so invalidate and refetch rather than patching
 * local state. The queue is small (bounded by failed runs awaiting review), so
 * a full refetch is cheap.
 */
export function onPostmortemInvalidate(qc: QueryClient, wsId: string) {
  qc.invalidateQueries({ queryKey: postmortemKeys.all(wsId) });
}
