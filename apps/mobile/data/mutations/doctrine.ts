/**
 * Workspace doctrine review + report resolution (OS plan, chantier 22).
 *
 * Non-optimistic on purpose: the server, not this client, decides whether a
 * review or a resolution applies (a proposal may have been reviewed by
 * another manager a second earlier — 409 "this version is not awaiting
 * review" / "this report is already resolved"), and an approval moves the
 * live revision, the ledger, every diff and the reviewer's inbox at once.
 * That is condition-for-condition the opposite of the optimistic-update
 * checklist in the root CLAUDE.md, so settle just invalidates.
 *
 * The inbox list is invalidated too: a `doctrine_review` / `doctrine_report`
 * notification stops being actionable once the thing it points at is
 * resolved (same cross-cache reasoning as use-calendar-realtime.ts).
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/data/api";
import { doctrineKeys } from "@/data/queries/doctrine";
import { inboxKeys } from "@/data/queries/inbox";
import { useWorkspaceStore } from "@/data/workspace-store";

function useDoctrineInvalidate() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return () => {
    qc.invalidateQueries({ queryKey: doctrineKeys.all(wsId) });
    qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
  };
}

export function useReviewDoctrineVersion() {
  const invalidate = useDoctrineInvalidate();
  return useMutation({
    mutationFn: (v: {
      id: string;
      decision: "approve" | "reject";
      note?: string;
    }) => api.reviewDoctrineVersion(v.id, v.decision, v.note),
    onSettled: invalidate,
  });
}

export function useResolveDoctrineReport() {
  const invalidate = useDoctrineInvalidate();
  return useMutation({
    mutationFn: (v: {
      id: string;
      resolution: "acknowledge" | "dismiss";
      note?: string;
    }) => api.resolveDoctrineReport(v.id, v.resolution, v.note),
    onSettled: invalidate,
  });
}
