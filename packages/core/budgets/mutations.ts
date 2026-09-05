import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type { CreateBudgetPolicyRequest, UpdateBudgetPolicyRequest } from "./schemas";
import { budgetKeys } from "./queries";

function useBudgetMutation<T>(mutationFn: (value: T) => Promise<unknown>) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({ mutationFn, onSettled: () => qc.invalidateQueries({ queryKey: budgetKeys.all(wsId) }) });
}

export function useCreateBudgetPolicy() {
  return useBudgetMutation((data: CreateBudgetPolicyRequest) => api.createBudgetPolicy(data));
}

export function useUpdateBudgetPolicy() {
  return useBudgetMutation(({ id, ...data }: { id: string } & UpdateBudgetPolicyRequest) => api.updateBudgetPolicy(id, data));
}

export function useDeleteBudgetPolicy() {
  return useBudgetMutation((id: string) => api.deleteBudgetPolicy(id));
}

export function useCreateBudgetOverride() {
  return useBudgetMutation(({ id, reason, durationHours = 24 }: { id: string; reason: string; durationHours?: number }) =>
    api.createBudgetOverride(id, reason, durationHours));
}
