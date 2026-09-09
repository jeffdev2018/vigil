/**
 * Goal loop (long tasks): the goal an issue is worked toward and the state
 * of the run chain pursuing it. Key shape mirrors `agentEffectKeys` —
 * `["issue-goal", wsId, issueId]` — three segments, `.all` clears the
 * workspace, `.issue` targets one issue.
 *
 * No web `packages/core/issues/*` counterpart exists yet (goal loop is
 * being built for web in the same PR wave); this file is the source of
 * truth for the key shape until that lands, at which point it should mirror
 * whatever key web settles on.
 */
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const issueGoalKeys = {
  all: (wsId: string | null) => ["issue-goal", wsId] as const,
  issue: (wsId: string | null, issueId: string) =>
    [...issueGoalKeys.all(wsId), issueId] as const,
};

export const issueGoalOptions = (wsId: string | null, issueId: string) =>
  queryOptions({
    queryKey: issueGoalKeys.issue(wsId, issueId),
    queryFn: ({ signal }) => api.getIssueGoal(issueId, { signal }),
    enabled: !!wsId && !!issueId,
    select: (res) => res.goal,
  });
