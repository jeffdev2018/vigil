import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { postmortemKeys } from "./queries";

/**
 * Approve and discard are deliberately NOT optimistic: each transitions a
 * postmortem out of the draft queue, so the caller awaits the server and we
 * invalidate after settle. CLAUDE.md rule: flows that change state await the
 * server before proceeding.
 */
export function useApprovePostmortem(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.approvePostmortem(id),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: postmortemKeys.all(wsId) });
    },
  });
}

export function useDiscardPostmortem(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.discardPostmortem(id),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: postmortemKeys.all(wsId) });
    },
  });
}
