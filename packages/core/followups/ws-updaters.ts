import type { QueryClient } from "@tanstack/react-query";
import { runKeys } from "../runs/fleet-queries";
import { followupKeys } from "./queries";

/**
 * `followup:changed` — one was scheduled or cancelled, by this client, another
 * member, an agent run, the CLI or MCP. The pending list is ordered and
 * budgeted server-side, and the same row shows in the runs fleet as a run
 * blocked on "deferred", so both projections refetch.
 *
 * Without an `issue_id` (a payload from a newer server) the whole workspace
 * prefix goes rather than the wrong issue's entry.
 */
export function onFollowupChanged(qc: QueryClient, wsId: string, issueId?: string): void {
  qc.invalidateQueries({
    queryKey: issueId ? followupKeys.issue(wsId, issueId) : followupKeys.all(wsId),
  });
  qc.invalidateQueries({ queryKey: runKeys.all(wsId) });
}
