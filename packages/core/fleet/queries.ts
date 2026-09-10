import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

// Fleet reads + consults (JEF-12). The three fleet queries are workspace
// aggregations; consults-by-task feeds the execution log's per-run lines.

export interface FleetWindow {
  /** RFC3339 or YYYY-MM-DD; omitted = the server's default 30-day window. */
  since?: string;
  agentId?: string;
}

export const fleetKeys = {
  all: (wsId: string) => ["fleet", wsId] as const,
  status: (wsId: string, window: FleetWindow = {}) =>
    [...fleetKeys.all(wsId), "status", window.since ?? "", window.agentId ?? ""] as const,
  cost: (wsId: string, window: FleetWindow = {}) =>
    [...fleetKeys.all(wsId), "cost", window.since ?? "", window.agentId ?? ""] as const,
  history: (wsId: string, window: FleetWindow = {}) =>
    [...fleetKeys.all(wsId), "history", window.since ?? "", window.agentId ?? ""] as const,
  consultsByTask: (wsId: string, taskId: string) =>
    [...fleetKeys.all(wsId), "consults", taskId] as const,
};

export function fleetStatusOptions(wsId: string, window: FleetWindow = {}) {
  return queryOptions({
    queryKey: fleetKeys.status(wsId, window),
    queryFn: () => api.getFleetStatus(window),
    enabled: !!wsId,
  });
}

export function fleetCostOptions(wsId: string, window: FleetWindow = {}) {
  return queryOptions({
    queryKey: fleetKeys.cost(wsId, window),
    queryFn: () => api.getFleetCost(window),
    enabled: !!wsId,
  });
}

export function fleetHistoryOptions(wsId: string, window: FleetWindow = {}) {
  return queryOptions({
    queryKey: fleetKeys.history(wsId, window),
    queryFn: () => api.getFleetHistory(window),
    enabled: !!wsId,
  });
}

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
