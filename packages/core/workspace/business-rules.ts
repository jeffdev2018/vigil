import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";

// Business rules (K53): plain-language rules compiled once, enforced
// deterministically at a fixed attach point.

export const businessRuleKeys = {
  list: (wsId: string) => ["business-rules", wsId] as const,
  violations: (wsId: string, ruleId: string) => ["business-rules", wsId, "violations", ruleId] as const,
};

export function businessRulesOptions(wsId: string) {
  return queryOptions({ queryKey: businessRuleKeys.list(wsId), queryFn: () => api.listBusinessRules() });
}

export function businessRuleViolationsOptions(wsId: string, ruleId: string) {
  return queryOptions({ queryKey: businessRuleKeys.violations(wsId, ruleId), queryFn: () => api.listBusinessRuleViolations(ruleId), enabled: !!ruleId });
}

export function useCreateBusinessRule(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { natural_language: string; attach_point: string; title?: string; predicate?: unknown; action?: { kind: string; priority?: string; assignee_type?: string; assignee_id?: string } }) => api.createBusinessRule(v),
    onSettled: () => qc.invalidateQueries({ queryKey: businessRuleKeys.list(wsId) }),
  });
}

export function useDryRunBusinessRule() {
  return useMutation({ mutationFn: (id: string) => api.dryRunBusinessRule(id) });
}

export function useSetBusinessRuleStatus(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; status: "active" | "disabled" }) => api.setBusinessRuleStatus(v.id, v.status),
    onSettled: () => qc.invalidateQueries({ queryKey: businessRuleKeys.list(wsId) }),
  });
}

export function useDeleteBusinessRule(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteBusinessRule(id),
    onSettled: () => qc.invalidateQueries({ queryKey: businessRuleKeys.list(wsId) }),
  });
}
