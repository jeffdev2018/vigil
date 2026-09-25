import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/**
 * Packs (OS plan, vague B). Every key is workspace-scoped: the catalogue
 * carries this workspace's install state (installed version, upgrade
 * available, prerequisite status), so it must never be shared across a
 * workspace switch.
 */
export const packKeys = {
  all: (wsId: string) => ["packs", wsId] as const,
  /** Not workspace-scoped: the built-in catalogue with no install state. */
  seedCatalogue: () => ["pack-catalogue"] as const,
  catalogue: (wsId: string) => [...packKeys.all(wsId), "catalogue"] as const,
  detail: (wsId: string, id: string) => [...packKeys.all(wsId), "detail", id] as const,
  installs: (wsId: string) => [...packKeys.all(wsId), "installs"] as const,
  install: (wsId: string, id: string) => [...packKeys.all(wsId), "install", id] as const,
};

/**
 * The pre-workspace catalogue: any authenticated user, no workspace header,
 * no install state. Keyed without a `wsId` on purpose — it is the same list
 * for everyone, and the create-workspace flow reads it before the workspace
 * it is seeding exists.
 */
export function packSeedCatalogueOptions() {
  return queryOptions({
    queryKey: packKeys.seedCatalogue(),
    queryFn: ({ signal }) => api.listPackCatalogue({ signal }),
  });
}

export function packCatalogueOptions(wsId: string) {
  return queryOptions({
    queryKey: packKeys.catalogue(wsId),
    queryFn: ({ signal }) => api.listPacks({ signal }),
    enabled: wsId.length > 0,
  });
}

export function packDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: packKeys.detail(wsId, id),
    queryFn: ({ signal }) => api.getPack(id, { signal }),
    enabled: wsId.length > 0 && id.length > 0,
  });
}

export function packInstallsOptions(wsId: string) {
  return queryOptions({
    queryKey: packKeys.installs(wsId),
    queryFn: ({ signal }) => api.listPackInstalls({ signal }),
    enabled: wsId.length > 0,
  });
}

export function packInstallOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: packKeys.install(wsId, id),
    queryFn: ({ signal }) => api.getPackInstall(id, { signal }),
    enabled: wsId.length > 0 && id.length > 0,
  });
}
