import { infiniteQueryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { AuditLogFilter } from "../types";

// Audit log (K08): a cursor-paginated, filtered read.

export const auditKeys = {
  list: (wsId: string, filter: AuditLogFilter) => ["audit-log", wsId, filter.since ?? "", filter.until ?? "", filter.actor_type ?? "", filter.action ?? ""] as const,
};

export function auditLogInfiniteOptions(wsId: string, filter: AuditLogFilter) {
  return infiniteQueryOptions({
    queryKey: auditKeys.list(wsId, filter),
    queryFn: ({ pageParam }) => api.listAuditLog(filter, pageParam || undefined),
    initialPageParam: "",
    getNextPageParam: (last) => (last.next_cursor ? last.next_cursor : undefined),
  });
}
