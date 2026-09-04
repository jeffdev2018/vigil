import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const budgetKeys = {
  all: (wsId: string) => ["budgets", wsId] as const,
  policies: (wsId: string) => [...budgetKeys.all(wsId), "policies"] as const,
  status: (wsId: string, scope: { projectId?: string; agentId?: string } = {}) =>
    [...budgetKeys.all(wsId), "status", scope.projectId ?? "", scope.agentId ?? ""] as const,
};

export function budgetPolicyOptions(wsId: string) {
  return queryOptions({ queryKey: budgetKeys.policies(wsId), queryFn: () => api.listBudgetPolicies(), enabled: !!wsId });
}

export function budgetStatusOptions(wsId: string, scope: { projectId?: string; agentId?: string } = {}) {
  return queryOptions({ queryKey: budgetKeys.status(wsId, scope), queryFn: () => api.getBudgetStatus(scope), enabled: !!wsId });
}
