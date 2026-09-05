/**
 * Executable org chart queries (K75). One shape for now: the list
 * (`orgKeys.list`) → `OrgStructure[]`, read-only on mobile.
 *
 * Key shape mirrors `packages/core/org/queries.ts` (`["org", wsId, "list"]`).
 * No realtime hook: the server emits no `org:*` WS events, so
 * pull-to-refresh is the only refresh path.
 */
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const orgKeys = {
  all: (wsId: string | null) => ["org", wsId] as const,
  list: (wsId: string | null) => [...orgKeys.all(wsId), "list"] as const,
};

export const orgListOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: orgKeys.list(wsId),
    queryFn: async ({ signal }) => {
      const res = await api.listOrgStructures({ signal });
      return res.structures;
    },
    enabled: !!wsId,
  });
