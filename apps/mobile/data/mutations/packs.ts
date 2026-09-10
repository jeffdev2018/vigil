/**
 * Pack preview / install / uninstall (OS plan, vague B).
 *
 * Nothing here is optimistic, for the same reasons as web
 * (`packages/core/packs/mutations.ts`): an install runs the transfer
 * pipeline server-side and what it creates, merges or skips per kind is a
 * server decision, and an uninstall decides row by row what it may remove.
 * Both await the server and invalidate on settle — condition-for-condition
 * the opposite of the optimistic-update gate in the root CLAUDE.md.
 *
 * `invalidatePackTargets` is the mobile counterpart of the exported web
 * helper: one bundle may create rows in any of these collections, so an
 * install or an uninstall refetches all of them. Mirrored, not imported —
 * mobile's key factories are different runtime instances (see
 * apps/mobile/CLAUDE.md "Mobile-owned updaters"), and mobile has no skills
 * or autopilots cache to clear.
 */
import {
  useMutation,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";
import { api } from "@/data/api";
import { agentKeys } from "@/data/queries/agents";
import { doctrineKeys } from "@/data/queries/doctrine";
import { goalKeys } from "@/data/queries/goals";
import { issueKeys } from "@/data/queries/issue-keys";
import { issueStatusKeys } from "@/data/queries/issue-statuses";
import { labelKeys } from "@/data/queries/labels";
import { orgKeys } from "@/data/queries/org";
import { packKeys } from "@/data/queries/packs";
import { projectKeys } from "@/data/queries/projects";
import { useWorkspaceStore } from "@/data/workspace-store";
import type { PackStrategy } from "@/data/schemas";

export function invalidatePackTargets(
  qc: QueryClient,
  wsId: string | null,
): void {
  for (const key of [
    packKeys.all(wsId),
    agentKeys.all(wsId),
    projectKeys.all(wsId),
    goalKeys.all(wsId),
    orgKeys.all(wsId),
    issueKeys.all(wsId),
    issueStatusKeys.all(wsId),
    labelKeys.all(wsId),
    doctrineKeys.all(wsId),
  ]) {
    qc.invalidateQueries({ queryKey: key });
  }
}

function usePackInvalidate() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return () => invalidatePackTargets(qc, wsId);
}

/**
 * Dry run. Not cached on purpose: the collisions it reports are only true
 * for the workspace as it stands right now, so the screen re-runs it rather
 * than showing a preview computed before someone else's change.
 */
export function usePreviewPack() {
  return useMutation({
    mutationFn: (v: { id: string; strategy?: PackStrategy }) =>
      api.previewPack(v.id, v.strategy),
  });
}

export function useInstallPack() {
  const invalidate = usePackInvalidate();
  return useMutation({
    mutationFn: (v: {
      id: string;
      strategy?: PackStrategy;
      force?: boolean;
    }) => api.installPack(v.id, v.strategy, v.force),
    onSettled: invalidate,
  });
}

/** Removes the configuration the pack created; the content it brought stays. */
export function useUninstallPack() {
  const invalidate = usePackInvalidate();
  return useMutation({
    mutationFn: (v: { id: string }) => api.uninstallPack(v.id),
    onSettled: invalidate,
  });
}
