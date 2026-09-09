import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const runHaltKeys = {
  all: (wsId: string) => ["run-halt", wsId] as const,
};

/** Whether this workspace's agents are held right now, who, and why. */
export function runHaltOptions(wsId: string) {
  return queryOptions({
    queryKey: runHaltKeys.all(wsId),
    queryFn: () => api.getRunHalt(),
    enabled: !!wsId,
    refetchInterval: 60_000,
  });
}
