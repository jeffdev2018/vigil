/**
 * Triage write mutations: accept / dismiss / reopen.
 *
 * Deliberately NOT optimistic. Each of the three moves an item between two
 * state lists (pending → accepted / dismissed / back to pending), which is a
 * cross-cache move rather than a field patch, and accept additionally creates
 * an issue server-side. That fails the mobile optimistic gate in
 * apps/mobile/CLAUDE.md ("outcome locally predictable, rollback trivial"):
 * the destination list is a different infinite query whose page boundaries we
 * cannot predict. Every call awaits the server, then invalidates.
 *
 * Cross-feature invalidation mirrors web `packages/core/triage/mutations.ts`:
 * accepting produces a real issue, so the issue lists must refresh or the
 * user lands on a workspace list that does not contain the issue they just
 * created.
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type {
  AcceptTriageItemResponse,
  DismissTriageItemResponse,
} from "@multica/core/types";
import { api } from "@/data/api";
import { triageKeys } from "@/data/queries/triage";
import { issueKeys } from "@/data/queries/issue-keys";
import { useWorkspaceStore } from "@/data/workspace-store";

/** Invalidates every triage list + the stats badge for the workspace. */
function useInvalidateTriage() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return () => {
    if (!wsId) return;
    // Prefix invalidate: covers every per-state items query plus stats.
    qc.invalidateQueries({ queryKey: triageKeys.all(wsId) });
  };
}

export function useAcceptTriageItem() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const invalidateTriage = useInvalidateTriage();

  return useMutation<AcceptTriageItemResponse, Error, string>({
    mutationFn: (itemId) => api.acceptTriageItem(itemId),
    onSuccess: () => {
      invalidateTriage();
      if (!wsId) return;
      // The accept created an issue. Web invalidates the whole issue prefix
      // here (packages/core/triage/mutations.ts useAcceptTriageItem) — mobile
      // mirrors that rather than guessing which lists the new issue joins.
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

export function useDismissTriageItem() {
  const invalidateTriage = useInvalidateTriage();
  return useMutation<DismissTriageItemResponse, Error, string>({
    mutationFn: (itemId) => api.dismissTriageItem(itemId),
    onSuccess: invalidateTriage,
  });
}

export function useReopenTriageItem() {
  const invalidateTriage = useInvalidateTriage();
  return useMutation<void, Error, string>({
    mutationFn: (itemId) => api.reopenTriageItem(itemId),
    onSuccess: invalidateTriage,
  });
}
