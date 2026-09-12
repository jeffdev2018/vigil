import { useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { autopilotKeys } from "../autopilots/queries";
import { doctrineKeys } from "../doctrine/queries";
import { goalKeys } from "../goals/queries";
import { issueKeys } from "../issues/queries";
import { orgKeys } from "../org/queries";
import { projectKeys } from "../projects/queries";
import { workspaceKeys } from "../workspace/queries";
import { saveTransferBlob } from "../workspace/transfer";
import { packKeys } from "./queries";
import type { PackExportInput, PackInstall, PackStrategy } from "./schemas";

/**
 * Packs (OS plan, vague B). Nothing here is optimistic: an install runs the
 * transfer pipeline server-side and its outcome (created / merged / skipped
 * per kind, warnings) is not locally predictable, and an uninstall decides
 * row by row what it can remove. Each awaits the server and invalidates on
 * settle (CLAUDE.md state rules).
 */

/**
 * A pack bundle may create rows in any of these collections, so an install or
 * an uninstall refetches all of them. Exported because the `pack:changed`
 * realtime listener has to do exactly the same work for the clients that did
 * not run the mutation.
 */
export function invalidatePackTargets(qc: QueryClient, wsId: string): void {
  for (const key of [
    packKeys.all(wsId),
    workspaceKeys.agents(wsId),
    workspaceKeys.skills(wsId),
    projectKeys.all(wsId),
    goalKeys.all(wsId),
    autopilotKeys.all(wsId),
    orgKeys.all(wsId),
    issueKeys.all(wsId),
    doctrineKeys.all(wsId),
  ]) {
    qc.invalidateQueries({ queryKey: key });
  }
}

/** Preview a catalogue pack: collisions, problems, the default strategy. */
export function usePreviewPack() {
  return useMutation({
    mutationFn: (v: { id: string; strategy?: PackStrategy }) =>
      api.previewPack(v.id, v.strategy),
  });
}

/** Preview an uploaded pack.yaml. The server parses it; we never read the YAML. */
export function usePreviewPackUpload() {
  return useMutation({
    mutationFn: (v: { file: File | Blob; strategy?: PackStrategy }) =>
      api.previewPackUpload(v.file, v.strategy),
  });
}

export function useInstallPack(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; strategy?: PackStrategy; force?: boolean }) =>
      api.installPack(v.id, v.strategy, v.force),
    onSettled: () => invalidatePackTargets(qc, wsId),
  });
}

export function useInstallPackUpload(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { file: File | Blob; strategy?: PackStrategy; force?: boolean }) =>
      api.installPackUpload(v.file, v.strategy, v.force),
    onSettled: () => invalidatePackTargets(qc, wsId),
  });
}

/**
 * Removes the configuration the pack created; content it brought stays.
 *
 * The response carries the ledger row as the server left it (status
 * `removed`, `removed_at` set), so it is written straight into the installs
 * list: the row loses its Installed badge and its Uninstall button the moment
 * the mutation resolves, instead of only once the refetch below lands. That
 * matters because the row's actions are what a stale copy makes wrong — the
 * server answers 409 to a second uninstall. The invalidation still runs on
 * settle for everything a pack touched, the catalogue included.
 */
export function useUninstallPack(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string }) => api.uninstallPack(v.id),
    onSuccess: (result) => {
      qc.setQueryData<PackInstall[]>(packKeys.installs(wsId), (rows) =>
        rows?.map((row) => (row.id === result.install.id ? result.install : row)),
      );
    },
    onSettled: () => invalidatePackTargets(qc, wsId),
  });
}

/** Hands the workspace's configuration to the browser as a pack.yaml download. */
export function useExportPack() {
  return useMutation({
    mutationFn: async (input: PackExportInput) => {
      const { blob, filename } = await api.exportPack(input);
      saveTransferBlob(blob, filename);
      return filename;
    },
  });
}

/** Downloads a catalogue pack's own pack.yaml (auth rides on the api client). */
export function useDownloadPack() {
  return useMutation({
    mutationFn: async (id: string) => {
      const { blob, filename } = await api.downloadPack(id);
      saveTransferBlob(blob, filename);
      return filename;
    },
  });
}
