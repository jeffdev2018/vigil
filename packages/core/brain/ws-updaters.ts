import type { QueryClient } from "@tanstack/react-query";
import { brainKeys } from "./queries";

/**
 * Refresh the whole Brain projection. create / update / delete all change the
 * server-side ordering (pinned first, then updated_at) and the tag facets, and
 * the server owns both — so invalidate and refetch rather than patching. The
 * corpus is bounded by what a team writes by hand plus what its runs save, so
 * a full refetch is cheap.
 */
export function onWorkspaceNoteInvalidate(qc: QueryClient, wsId: string) {
  qc.invalidateQueries({ queryKey: brainKeys.all(wsId) });
}
