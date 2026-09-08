import { useQuery } from "@tanstack/react-query";
import { useCallback, useMemo } from "react";
import { buildIssueTypeCatalog, issueTypeListOptions, type IssueTypeCatalog } from "./queries";

/**
 * The workspace work item type catalogue, resolved and memoized (F30).
 *
 * Takes `wsId` explicitly rather than reading it from context, per the repo's
 * state rules — the catalogue is per-workspace, and switching workspaces does
 * not remount the app, so a module-level snapshot would go stale.
 */
export function useIssueTypes(wsId: string): IssueTypeCatalog {
  const { data, isPending, isError, refetch } = useQuery({
    ...issueTypeListOptions(wsId),
    enabled: Boolean(wsId),
  });
  const retry = useCallback(() => {
    void refetch();
  }, [refetch]);
  return useMemo(
    () => buildIssueTypeCatalog(data, { isPending, isError, retry }),
    [data, isPending, isError, retry],
  );
}
