import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

// Data residency (K46). The policy is workspace state and changes rarely, so
// it is a plain query with no polling; the runtimes list carries each runtime's
// declaration inline, so no second query is needed to render compliance.

export const residencyKeys = {
  policy: (wsId: string) => ["data-residency", wsId] as const,
};

export function dataResidencyOptions(wsId: string) {
  return queryOptions({
    queryKey: residencyKeys.policy(wsId),
    queryFn: () => api.getDataResidencyPolicy(),
    enabled: !!wsId,
  });
}
