/**
 * Postmortem approve / discard.
 *
 * Mirrors web `packages/core/postmortem/mutations.ts`: non-optimistic, and
 * the only cache touched is the postmortem prefix — approving does NOT
 * refresh issues, inbox or agents on web either. The approve response
 * carries `applied_rules` (how many preventive rules were copied into the
 * agent's memory), which the caller surfaces.
 *
 * Both write the returned postmortem into the detail cache before
 * invalidating, so the screen the user is looking at flips to its resolved
 * state in the same frame instead of after the refetch.
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { Postmortem } from "@multica/core/types";
import { api } from "@/data/api";
import { postmortemKeys } from "@/data/queries/postmortem";
import { useWorkspaceStore } from "@/data/workspace-store";

function usePostmortemResolve(
  resolve: (id: string) => Promise<Postmortem | null>,
) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation<Postmortem | null, Error, string>({
    mutationFn: resolve,
    onSuccess: (updated, id) => {
      if (!wsId) return;
      if (updated) {
        qc.setQueryData(postmortemKeys.detail(wsId, id), updated);
      }
      // Prefix invalidate: the item moved from `items(draft)` into
      // `items(approved|discarded)` and every stats bucket shifted.
      qc.invalidateQueries({ queryKey: postmortemKeys.all(wsId) });
    },
  });
}

export function useApprovePostmortem() {
  return usePostmortemResolve((id) => api.approvePostmortem(id));
}

export function useDiscardPostmortem() {
  return usePostmortemResolve((id) => api.discardPostmortem(id));
}
