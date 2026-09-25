import type { QueryClient } from "@tanstack/react-query";
import { brainCaptureKeys, brainKeys } from "./queries";

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

/**
 * A capture changed: it was created, transcribed, suggested, organized,
 * discarded, reopened or deleted. The inbox ordering, the raw badge and any
 * open capture are all server-owned, so refetch the capture projection.
 *
 * `note` and `merge` also wrote a note, which reshuffles the note list, its
 * tag facets and every ranked-search hit — so those go too. A `suggested` or
 * `transcribed` event must NOT drag them along: the model finishing its read
 * of a capture changes nothing a note query returns.
 */
export function onBrainCaptureChanged(
  qc: QueryClient,
  wsId: string,
  change?: string,
) {
  if (change === "note" || change === "merge") {
    qc.invalidateQueries({ queryKey: brainKeys.all(wsId) });
    return;
  }
  qc.invalidateQueries({ queryKey: brainCaptureKeys.captures(wsId) });
}
