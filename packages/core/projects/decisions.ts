import { queryOptions } from "@tanstack/react-query";
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
