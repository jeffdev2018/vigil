import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

// Agent consults (JEF-12), feeding the execution log's per-run lines.

export const fleetKeys = {
  all: (wsId: string) => ["fleet", wsId] as const,
  consultsByTask: (wsId: string, taskId: string) =>
    [...fleetKeys.all(wsId), "consults", taskId] as const,
};

// A run's consults, in call order. Most runs have none, so the query result
// is usually an empty list; callers render nothing for it.
export function taskConsultsOptions(wsId: string, taskId: string) {
  return queryOptions({
    queryKey: fleetKeys.consultsByTask(wsId, taskId),
    queryFn: () => api.listTaskConsults(taskId),
    enabled: !!wsId && !!taskId,
    staleTime: 30_000,
  });
}
