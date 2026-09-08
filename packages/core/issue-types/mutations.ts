import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { propertyKeys } from "../properties/queries";
import { compareIssueTypeEntries, issueTypeKeys } from "./queries";
import type {
  CreateIssueTypeRequest,
  IssueTypeEntry,
  ListIssueTypesResponse,
  UpdateIssueTypeRequest,
} from "../types";

/**
 * Work item type catalogue mutations (F30).
 *
 * Same contract as the status catalogue's (MUL-6458): no mutation invalidates
 * on success, because the `issue_type:changed` realtime event reaches the
 * writing tab too and one catalogue edit should cost each client exactly ONE
 * read. And no mutation writes its RESPONSE BODY into the cache — a response is
 * authoritative as of that write, not as of now, so a slow one landing after a
 * refetch would roll a concurrent edit back. Every local write below is instead
 * something that stays true regardless of what else has landed: an optimistic
 * field patch, an insert of a row nobody else can have seen, or a monotonic
 * flag (archive; there is no unarchive endpoint).
 *
 * On FAILURE the invalidate stays: a 409 usually means this client's catalogue
 * is the stale thing.
 */
function useTypeCatalogCache() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const listKey = issueTypeKeys.list(wsId);

  const writeTypes = (
    update: (types: IssueTypeEntry[]) => IssueTypeEntry[] | null,
    totalDelta = 0,
  ) => {
    qc.setQueryData<ListIssueTypesResponse>(listKey, (old) => {
      if (!old) return old;
      const types = update(old.types);
      if (!types) return old;
      return { ...old, types: types.sort(compareIssueTypeEntries), total: old.total + totalDelta };
    });
  };

  return {
    qc,
    listKey,
    insertEntry: (entry: IssueTypeEntry) => {
      // A response that failed schema validation degrades to an empty stub
      // (`parseWithFallback`). Writing it would put a blank row in the picker;
      // leaving the cache alone lets the realtime refetch supply the truth.
      if (!entry?.id) return;
      writeTypes((types) => (types.some((t) => t.id === entry.id) ? null : [...types, entry]), 1);
    },
    patchEntry: (id: string, patch: Partial<IssueTypeEntry>) => {
      writeTypes((types) =>
        types.some((t) => t.id === id) ? types.map((t) => (t.id === id ? { ...t, ...patch } : t)) : null,
      );
    },
    invalidate: () => {
      qc.invalidateQueries({ queryKey: issueTypeKeys.all(wsId) });
    },
  };
}

export function useCreateIssueType() {
  const { insertEntry, invalidate } = useTypeCatalogCache();
  return useMutation({
    mutationFn: (data: CreateIssueTypeRequest) => api.createIssueType(data),
    onSuccess: insertEntry,
    onError: invalidate,
  });
}

/** Optimistic rename / recolour / re-icon. Without it, an edit would snap back
 *  for the round-trip. */
export function useUpdateIssueType() {
  const { qc, listKey, patchEntry, invalidate } = useTypeCatalogCache();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateIssueTypeRequest) =>
      api.updateIssueType(id, data),
    onMutate: async ({ id, ...data }) => {
      await qc.cancelQueries({ queryKey: listKey });
      const previous = qc.getQueryData<ListIssueTypesResponse>(listKey);
      patchEntry(id, data);
      return { previous };
    },
    // Nothing on success, and deliberately NO issues invalidate: an issue row
    // stores the type KEY, and its name and colour are resolved from this
    // catalogue at render time, so refreshing the catalogue is what repaints
    // every badge. Pulling every board and list along would turn one rename
    // into a workspace-wide refetch storm.
    onError: (_err, _vars, ctx) => {
      if (ctx?.previous) qc.setQueryData(listKey, ctx.previous);
      invalidate();
    },
  });
}

/** Archives a custom type. Deliberately NOT optimistic: the server refuses to
 *  archive a system type, and a row vanishing before that refusal arrives would
 *  read as success. */
export function useArchiveIssueType() {
  const { patchEntry, invalidate } = useTypeCatalogCache();
  return useMutation({
    mutationFn: (id: string) => api.archiveIssueType(id),
    // Only `archived_at`, never the whole returned row: archiving is terminal,
    // so the flag stays true whatever else landed meanwhile, while a full
    // snapshot would revert a rename that raced this archive. The row itself
    // stays — issues sitting on it resolve their label through it.
    onSuccess: (entry) => {
      if (!entry?.id) return;
      patchEntry(entry.id, { archived_at: entry.archived_at });
    },
    onError: invalidate,
  });
}

/**
 * Commits a drag-reorder over the whole catalogue. One request, not a PATCH per
 * row: a sequence of writes is not atomic, so a row rejected part-way would
 * leave the rows before it already reordered while the caller is told the whole
 * operation failed.
 */
export function useReorderIssueTypes() {
  const { qc, listKey, invalidate } = useTypeCatalogCache();
  return useMutation({
    mutationFn: (ordered: IssueTypeEntry[]) => api.reorderIssueTypes(ordered.map((e) => e.id)),
    onMutate: async (ordered) => {
      await qc.cancelQueries({ queryKey: listKey });
      const previous = qc.getQueryData<ListIssueTypesResponse>(listKey);
      const positionById = new Map(ordered.map((e, index) => [e.id, index + 1]));
      qc.setQueryData<ListIssueTypesResponse>(listKey, (old) =>
        old
          ? {
              ...old,
              // Re-sorted, not just re-positioned: consumers render the array
              // in order, so writing positions alone would leave the drag
              // visually undone until the refetch lands.
              types: old.types
                .map((t) => (positionById.has(t.id) ? { ...t, position: positionById.get(t.id)! } : t))
                .sort(compareIssueTypeEntries),
            }
          : old,
      );
      return { previous };
    },
    // Reorder answers with the FULL catalogue, which is exactly why the
    // response is not installed: a whole-table snapshot arriving late
    // overwrites every row.
    onError: (_err, _vars, ctx) => {
      if (ctx?.previous) qc.setQueryData(listKey, ctx.previous);
      invalidate();
    },
  });
}

/**
 * Replaces a property's type scope. Invalidates the PROPERTY catalogue, not the
 * type one: the scope lives on the property, and every surface that decides
 * whether to render a property row reads it from there.
 */
export function useSetPropertyTypes() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, typeKeys }: { id: string; typeKeys: string[] }) =>
      api.setPropertyTypes(id, typeKeys),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: propertyKeys.all(wsId) });
    },
  });
}
