import type { QueryClient } from "@tanstack/react-query";
import { issueKeys } from "../issues/queries";
import { recurrenceKeys } from "./queries";

/**
 * `issue_recurrence:changed` — a rule was created, updated or cleared, by this
 * client, another member, the CLI or MCP. The rule is shared by the series, so
 * the whole workspace prefix goes rather than one issue's entry: the frame
 * names the issue it was filed from, not the occurrences that read the same
 * rule, and a recurrence entry is cheap enough that refetching the series
 * beats keeping a client-side map of who belongs to it.
 *
 * A clear detaches every past occurrence and a tick spawns a new one, so the
 * issue projections move with it.
 */
export function onIssueRecurrenceChanged(qc: QueryClient, wsId: string): void {
  qc.invalidateQueries({ queryKey: recurrenceKeys.all(wsId) });
  qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
}
