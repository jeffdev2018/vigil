import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { criticKeys } from "./queries";
import type { CriticPolicyWrite } from "./schemas";

/**
 * Never optimistic: the server refuses an enabled policy without a critic, and
 * showing the switch on before it answered would claim a guarantee that does
 * not exist yet.
 */
export function useSaveCriticPolicy(wsId: string, subjectType: string, subjectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: CriticPolicyWrite) => api.putCriticPolicy(subjectType, subjectId, data),
    onSettled: () =>
      qc.invalidateQueries({ queryKey: criticKeys.policy(wsId, subjectType, subjectId) }),
  });
}
