import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { DoctrineReportFilter } from "./schemas";

/**
 * Workspace doctrine (OS plan, chantier 22). Every key is workspace-scoped —
 * the doctrine is one document per workspace, and the settings tab can be
 * reached right after a workspace switch.
 */
export const doctrineKeys = {
  all: (wsId: string) => ["doctrine", wsId] as const,
  doctrine: (wsId: string) => [...doctrineKeys.all(wsId), "doctrine"] as const,
  versions: (wsId: string) => [...doctrineKeys.all(wsId), "versions"] as const,
  version: (wsId: string, id: string) => [...doctrineKeys.all(wsId), "version", id] as const,
  diff: (wsId: string, id: string, against: string) =>
    [...doctrineKeys.all(wsId), "diff", id, against] as const,
  reports: (wsId: string, status: DoctrineReportFilter) =>
    [...doctrineKeys.all(wsId), "reports", status] as const,
};

export function doctrineOptions(wsId: string) {
  return queryOptions({
    queryKey: doctrineKeys.doctrine(wsId),
    queryFn: ({ signal }) => api.getWorkspaceDoctrine({ signal }),
    enabled: wsId.length > 0,
  });
}

export function doctrineVersionsInfiniteOptions(wsId: string) {
  return infiniteQueryOptions({
    queryKey: doctrineKeys.versions(wsId),
    queryFn: ({ pageParam, signal }) =>
      api.listDoctrineVersions(pageParam || undefined, { signal }),
    initialPageParam: "",
    getNextPageParam: (last) => last.next_cursor || undefined,
    enabled: wsId.length > 0,
  });
}

/** `against` empty compares the version with its predecessor (server default). */
export function doctrineDiffOptions(wsId: string, id: string, against = "") {
  return queryOptions({
    queryKey: doctrineKeys.diff(wsId, id, against),
    queryFn: ({ signal }) => api.getDoctrineVersionDiff(id, against || undefined, { signal }),
    enabled: wsId.length > 0 && id.length > 0,
  });
}

export function doctrineReportsOptions(wsId: string, status: DoctrineReportFilter = "open") {
  return queryOptions({
    queryKey: doctrineKeys.reports(wsId, status),
    queryFn: ({ signal }) => api.listDoctrineReports(status, { signal }),
    enabled: wsId.length > 0,
  });
}
