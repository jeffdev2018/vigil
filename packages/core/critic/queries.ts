import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const criticKeys = {
  policy: (wsId: string, subjectType: string, subjectId: string) =>
    ["critic-policy", wsId, subjectType, subjectId] as const,
  // Prefix-shaped on purpose: the realtime handler invalidates
  // ["critic-verdicts", wsId] without knowing which issue changed.
  verdicts: (wsId: string, issueId: string) =>
    ["critic-verdicts", wsId, issueId] as const,
};

export function criticPolicyOptions(wsId: string, subjectType: string, subjectId: string) {
  return queryOptions({
    queryKey: criticKeys.policy(wsId, subjectType, subjectId),
    queryFn: () => api.getCriticPolicy(subjectType, subjectId),
    enabled: !!wsId && !!subjectId,
  });
}

export function criticVerdictsOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: criticKeys.verdicts(wsId, issueId),
    queryFn: () => api.listCriticVerdicts(issueId),
    enabled: !!wsId && !!issueId,
  });
}
