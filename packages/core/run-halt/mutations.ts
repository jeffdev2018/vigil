import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { approvalKeys } from "../approvals/queries";
import { runHaltKeys } from "./queries";

/**
 * Set or lift the workspace's fleet halt. Never optimistic: refusing every
 * gated action is exactly the kind of thing that must reflect what the
 * server actually holds, not what this client hoped to set.
 */
export function useSetRunHalt(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { halted: boolean; reason: string }) => api.putRunHalt(input),
    onSuccess: (data) => qc.setQueryData(runHaltKeys.all(wsId), data),
    // The approvals feed embeds run_halt too — refresh it alongside so a
    // banner and the feed's own copy never disagree.
    onSettled: () => qc.invalidateQueries({ queryKey: approvalKeys.all(wsId) }),
  });
}
