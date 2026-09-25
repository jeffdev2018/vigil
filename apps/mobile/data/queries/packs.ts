/**
 * Packs cache keys + query options (OS plan, vague B).
 *
 * Mirrors `packages/core/packs/queries.ts` key-for-key: every key is
 * workspace-scoped because the catalogue carries *this* workspace's install
 * state (installed version, upgrade available, prerequisite status), so it
 * must never survive a workspace switch.
 *
 * The pre-workspace seed catalogue (`GET /api/pack-catalogue`) is
 * deliberately absent: creating a workspace is a web/desktop flow, so the
 * phone never picks a seed pack.
 */
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const packKeys = {
  all: (wsId: string | null) => ["packs", wsId] as const,
  catalogue: (wsId: string | null) =>
    [...packKeys.all(wsId), "catalogue"] as const,
  detail: (wsId: string | null, id: string) =>
    [...packKeys.all(wsId), "detail", id] as const,
  installs: (wsId: string | null) =>
    [...packKeys.all(wsId), "installs"] as const,
  install: (wsId: string | null, id: string) =>
    [...packKeys.all(wsId), "install", id] as const,
};

/** The ten-pack catalogue with this workspace's install state. */
export const packCatalogueOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: packKeys.catalogue(wsId),
    queryFn: ({ signal }) => api.listPacks({ signal }),
    enabled: !!wsId,
  });

/** One pack: manifest, prerequisite status and the contents by kind. */
export const packDetailOptions = (wsId: string | null, id: string) =>
  queryOptions({
    queryKey: packKeys.detail(wsId, id),
    queryFn: ({ signal }) => api.getPack(id, { signal }),
    enabled: !!wsId && id.length > 0,
  });

/** The install ledger, one row per install (including removed ones). */
export const packInstallsOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: packKeys.installs(wsId),
    queryFn: ({ signal }) => api.listPackInstalls({ signal }),
    enabled: !!wsId,
  });

/** One install plus every row it created — mounted only while expanded. */
export const packInstallOptions = (wsId: string | null, id: string) =>
  queryOptions({
    queryKey: packKeys.install(wsId, id),
    queryFn: ({ signal }) => api.getPackInstall(id, { signal }),
    enabled: !!wsId && id.length > 0,
  });
