/**
 * Undo for agent actions (K69): the side effects agent runs produced on an
 * issue. Key shape mirrors web `agentEffectKeys.issue` in
 * packages/core/issues/agent-effects.ts — `["agent-effects", wsId, issueId]`.
 */
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const agentEffectKeys = {
  all: (wsId: string | null) => ["agent-effects", wsId] as const,
  issue: (wsId: string | null, issueId: string) =>
    [...agentEffectKeys.all(wsId), issueId] as const,
};

export const issueAgentEffectsOptions = (wsId: string | null, issueId: string) =>
  queryOptions({
    queryKey: agentEffectKeys.issue(wsId, issueId),
    queryFn: ({ signal }) => api.listIssueAgentEffects(issueId, { signal }),
    enabled: !!wsId && !!issueId,
  });
