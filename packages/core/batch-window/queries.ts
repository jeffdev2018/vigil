import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

// Off-peak batch lane (K45). The window is workspace state and changes rarely,
// so it is a plain query with no polling.

export const batchWindowKeys = {
  window: (wsId: string) => ["batch-window", wsId] as const,
};

export function batchWindowOptions(wsId: string) {
  return queryOptions({
    queryKey: batchWindowKeys.window(wsId),
    queryFn: () => api.getBatchWindow(),
    enabled: !!wsId,
  });
}
