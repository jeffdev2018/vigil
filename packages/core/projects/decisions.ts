import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";

// Decision memory (K29): the decisions recorded on a project's issues.

export type DecisionAuthorFilter = "" | "agent" | "member";

export const decisionKeys = {
  project: (wsId: string, projectId: string, author: DecisionAuthorFilter) => ["decisions", wsId, projectId, author] as const,
};

export function projectDecisionsOptions(wsId: string, projectId: string, author: DecisionAuthorFilter = "") {
  return queryOptions({
    queryKey: decisionKeys.project(wsId, projectId, author),
    queryFn: () => api.listProjectDecisions(projectId, author || undefined),
    enabled: !!projectId,
  });
}

/**
 * Record a decision on an issue by hand.
 *
 * Not optimistic: the endpoint validates the cited message against the run and
 * can refuse 422, the dialog stays open on failure, and the new record has a
 * server-assigned id — none of the conditions for an optimistic patch hold
 * (CLAUDE.md "State Rules"). Every project decision list is invalidated on
 * success because the author filter splits one project across three cache
 * entries and the caller cannot know which the user is looking at. Scoped to
 * the workspace: a decision recorded here cannot change another one.
 */
export function useCreateIssueDecision(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: {
      issueId: string;
      runId?: string;
      decision: {
        source_message_seq: number;
        title: string;
        decision: string;
        context?: string;
        consequences?: string;
      };
    }) =>
      api.createIssueDecisions(input.issueId, {
        ...(input.runId ? { run_id: input.runId } : {}),
        decisions: [input.decision],
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["decisions", wsId] });
    },
  });
}
