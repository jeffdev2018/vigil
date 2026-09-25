/**
 * Workspace doctrine cache keys + query options (OS plan, chantier 22).
 *
 * Parity target is `server/internal/handler/workspace_doctrine.go`: there is
 * no packages/views doctrine page to mirror yet (the doctrine landed
 * server-first), so the endpoints, the enums and the permission signal
 * (`can_publish`) come straight from the handler.
 *
 * Three-segment key shape per apps/mobile/CLAUDE.md "Query / mutation
 * factory pattern": `.all` lets `doctrine:changed` (or a workspace switch)
 * clear the doctrine, the ledger, every diff and the report lists in one
 * call; the sub-keys let a screen target its own slice.
 */
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export type DoctrineReportFilter =
  | "open"
  | "acknowledged"
  | "dismissed"
  | "all";

export const doctrineKeys = {
  all: (wsId: string | null) => ["doctrine", wsId] as const,
  detail: (wsId: string | null) => [...doctrineKeys.all(wsId), "detail"] as const,
  versions: (wsId: string | null) =>
    [...doctrineKeys.all(wsId), "versions"] as const,
  diff: (wsId: string | null, id: string | null) =>
    [...doctrineKeys.all(wsId), "diff", id] as const,
  reports: (wsId: string | null, status: DoctrineReportFilter) =>
    [...doctrineKeys.all(wsId), "reports", status] as const,
};

/** The live doctrine, its revision, the pending proposal and the open-report
 *  count — the whole doctrine screen header plus the More-tab badge. */
export const doctrineOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: doctrineKeys.detail(wsId),
    queryFn: ({ signal }) => api.getDoctrine({ signal }),
    enabled: !!wsId,
  });

/** The revision ledger, newest first. First page only: the phone reads and
 *  reviews, restoring an old revision stays on web/desktop. */
export const doctrineVersionsOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: doctrineKeys.versions(wsId),
    queryFn: ({ signal }) => api.listDoctrineVersions(undefined, { signal }),
    enabled: !!wsId,
  });

export const doctrineVersionDiffOptions = (
  wsId: string | null,
  id: string | null,
) =>
  queryOptions({
    queryKey: doctrineKeys.diff(wsId, id),
    queryFn: ({ signal }) => api.getDoctrineVersionDiff(id!, { signal }),
    enabled: !!wsId && !!id,
  });

export const doctrineReportsOptions = (
  wsId: string | null,
  status: DoctrineReportFilter,
) =>
  queryOptions({
    queryKey: doctrineKeys.reports(wsId, status),
    queryFn: ({ signal }) => api.listDoctrineReports(status, { signal }),
    enabled: !!wsId,
  });
