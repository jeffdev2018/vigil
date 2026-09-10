/**
 * Workspace Brain cache keys + query options — shared notes, the capture
 * inbox and ranked search.
 *
 * Key shapes mirror `packages/core/brain/queries.ts` segment for segment
 * (`["brain", wsId]` → `"list"|"detail"|"captures"|"search"`) so a reader
 * switching between mobile and web finds the same prefixes, and so one
 * realtime event can clear exactly the projection it moved:
 *
 *   brainKeys.all(wsId)                → notes + captures + search
 *   brainCaptureKeys.captures(wsId)    → the inbox and any open capture only
 *
 * Mirrored, not imported: web's factory is a different runtime instance and
 * binding mobile's cache mutations to it invites silent drift (see
 * apps/mobile/CLAUDE.md "Mobile-owned updaters").
 */
import { keepPreviousData, queryOptions } from "@tanstack/react-query";
import type { BrainCaptureStatus } from "@multica/core/types";
import { api } from "@/data/api";

/** The capture filter values the server accepts, plus the "everything" one. */
export type BrainCaptureFilter = BrainCaptureStatus | "all";

export const brainKeys = {
  all: (wsId: string | null) => ["brain", wsId] as const,
  list: (
    wsId: string | null,
    search: string,
    tag: string,
    archived: boolean,
  ) => [...brainKeys.all(wsId), "list", search, tag, archived] as const,
  detail: (wsId: string | null, id: string) =>
    [...brainKeys.all(wsId), "detail", id] as const,
};

export const brainCaptureKeys = {
  captures: (wsId: string | null) =>
    [...brainKeys.all(wsId), "captures"] as const,
  list: (wsId: string | null, status: BrainCaptureFilter) =>
    [...brainCaptureKeys.captures(wsId), "list", status] as const,
  detail: (wsId: string | null, id: string) =>
    [...brainCaptureKeys.captures(wsId), "detail", id] as const,
  search: (
    wsId: string | null,
    q: string,
    tag: string,
    archived: boolean,
    limit: number,
  ) => [...brainKeys.all(wsId), "search", q, tag, archived, limit] as const,
};

/**
 * The note list. Search and tag are part of the key because the server owns
 * both filters, so each combination is its own cache entry — and
 * `keepPreviousData` stops the list AND the tag chips (including the chip the
 * user is about to tap) from blanking to the skeleton on every keystroke.
 */
export const brainNotesOptions = (
  wsId: string | null,
  params?: { search?: string; tag?: string; archived?: boolean },
) => {
  const search = params?.search ?? "";
  const tag = params?.tag ?? "";
  const archived = params?.archived === true;
  return queryOptions({
    queryKey: brainKeys.list(wsId, search, tag, archived),
    placeholderData: keepPreviousData,
    queryFn: ({ signal }) =>
      api.listWorkspaceNotes(
        {
          search: search || undefined,
          tag: tag || undefined,
          archived: archived || undefined,
        },
        { signal },
      ),
    enabled: !!wsId,
  });
};

/** One note. The detail screen reads this rather than the list entry so it
 *  survives a cold entry (deep link, notification) and a post-409 refetch. */
export const brainNoteOptions = (wsId: string | null, id: string) =>
  queryOptions({
    queryKey: brainKeys.detail(wsId, id),
    queryFn: ({ signal }) => api.getWorkspaceNote(id, { signal }),
    enabled: !!wsId && id.length > 0,
  });

/**
 * The capture inbox. `raw_count` rides along with every status filter (the
 * server always counts raw), which is what lets the More-popover badge share
 * this cache entry instead of paying its own request.
 */
export const brainCapturesOptions = (
  wsId: string | null,
  status: BrainCaptureFilter = "raw",
) =>
  queryOptions({
    queryKey: brainCaptureKeys.list(wsId, status),
    // Switching raw/organized/discarded must not blank the list and the raw
    // badge back to the skeleton.
    placeholderData: keepPreviousData,
    queryFn: ({ signal }) => api.listBrainCaptures({ status }, { signal }),
    enabled: !!wsId,
  });

export const brainCaptureOptions = (wsId: string | null, id: string) =>
  queryOptions({
    queryKey: brainCaptureKeys.detail(wsId, id),
    queryFn: ({ signal }) => api.getBrainCapture(id, { signal }),
    enabled: !!wsId && id.length > 0,
  });

/**
 * Ranked search. The caller debounces `q`; an empty query is not a search
 * (the plain list is), which `enabled` enforces so a cleared field falls
 * straight back to the listing instead of firing a 400.
 */
export const noteSearchOptions = (
  wsId: string | null,
  q: string,
  params?: { tag?: string; archived?: boolean; limit?: number },
) => {
  const tag = params?.tag ?? "";
  const archived = params?.archived === true;
  const limit = params?.limit ?? 20;
  const query = q.trim();
  return queryOptions({
    queryKey: brainCaptureKeys.search(wsId, query, tag, archived, limit),
    placeholderData: keepPreviousData,
    queryFn: ({ signal }) =>
      api.searchWorkspaceNotes(
        { q: query, tag: tag || undefined, archived, limit },
        { signal },
      ),
    enabled: !!wsId && query !== "",
  });
};
