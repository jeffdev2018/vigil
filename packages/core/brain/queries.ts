import { keepPreviousData, queryOptions, useQuery } from "@tanstack/react-query";
import { api } from "../api";
import type { BrainCaptureStatus, WorkspaceNoteKind } from "../types";

export const brainKeys = {
  all: (wsId: string) => ["brain", wsId] as const,
  list: (wsId: string, search: string, tag: string, archived: boolean, kind: string) =>
    [...brainKeys.all(wsId), "list", search, tag, archived, kind] as const,
  detail: (wsId: string, id: string) => [...brainKeys.all(wsId), "detail", id] as const,
  usage: (wsId: string, id: string) => [...brainKeys.all(wsId), "usage", id] as const,
  taskUsage: (wsId: string, taskId: string) =>
    [...brainKeys.all(wsId), "task-usage", taskId] as const,
};

/** The runs that used one note, and how (JEF-413). */
export function noteUsageOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: brainKeys.usage(wsId, id),
    queryFn: ({ signal }) => api.getWorkspaceNoteUsage(id, undefined, { signal }),
    enabled: wsId !== "" && id !== "",
  });
}

/** The Brain notes one run used. */
export function taskNoteUsageOptions(wsId: string, taskId: string) {
  return queryOptions({
    queryKey: brainKeys.taskUsage(wsId, taskId),
    queryFn: ({ signal }) => api.listTaskNoteUsage(taskId, { signal }),
    enabled: wsId !== "" && taskId !== "",
  });
}

/**
 * The Brain listing. Search and tag are part of the key because the server
 * owns both filters (full-text search runs on the GIN index, not in the
 * browser), so each combination is its own cache entry.
 */
export function brainNotesOptions(
  wsId: string,
  params?: { search?: string; tag?: string; archived?: boolean; kind?: WorkspaceNoteKind },
) {
  const search = params?.search ?? "";
  const tag = params?.tag ?? "";
  const archived = params?.archived === true;
  const kind = params?.kind ?? "";
  return queryOptions({
    queryKey: brainKeys.list(wsId, search, tag, archived, kind),
    // Each filter combination is its own cache entry, so without this every
    // keystroke would blank the list AND the tag chips back to the loading
    // skeleton — including the chip the user is about to click.
    placeholderData: keepPreviousData,
    queryFn: ({ signal }) =>
      api.listWorkspaceNotes(
        {
          search: search || undefined,
          tag: tag || undefined,
          archived: archived || undefined,
          kind: kind || undefined,
        },
        { signal },
      ),
  });
}

/** One note, for the detail pane's fresh read after a conflict. */
export function brainNoteOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: brainKeys.detail(wsId, id),
    queryFn: ({ signal }) => api.getWorkspaceNote(id, { signal }),
    enabled: id !== "",
  });
}

/** Capture-inbox and ranked-search keys, under the same workspace prefix. */
export const brainCaptureKeys = {
  /** Prefix every capture query hangs off, so one realtime event can refresh
   *  the inbox and any open capture without touching notes or search. */
  captures: (wsId: string) => [...brainKeys.all(wsId), "captures"] as const,
  list: (wsId: string, status: BrainCaptureStatus | "all") =>
    [...brainCaptureKeys.captures(wsId), "list", status] as const,
  detail: (wsId: string, id: string) =>
    [...brainCaptureKeys.captures(wsId), "detail", id] as const,
  search: (
    wsId: string,
    q: string,
    tag: string,
    archived: boolean,
    limit: number,
    kind: string,
  ) => [...brainKeys.all(wsId), "search", q, tag, archived, limit, kind] as const,
};

export function brainCapturesOptions(
  wsId: string,
  status: BrainCaptureStatus | "all" = "raw",
) {
  return queryOptions({
    queryKey: brainCaptureKeys.list(wsId, status),
    // Switching the raw/organized/discarded filter must not blank the inbox
    // (and its raw badge) back to the skeleton.
    placeholderData: keepPreviousData,
    queryFn: ({ signal }) => api.listBrainCaptures({ status }, { signal }),
  });
}

export function brainCaptureOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: brainCaptureKeys.detail(wsId, id),
    queryFn: ({ signal }) => api.getBrainCapture(id, { signal }),
    enabled: id !== "",
  });
}

/**
 * Ranked search. The caller debounces `q` — this fires on every distinct
 * query it is handed, and an empty query is not a search (the plain list is).
 */
export function noteSearchOptions(
  wsId: string,
  q: string,
  params?: { tag?: string; archived?: boolean; limit?: number; kind?: WorkspaceNoteKind },
) {
  const tag = params?.tag ?? "";
  const archived = params?.archived === true;
  const limit = params?.limit ?? 20;
  const kind = params?.kind ?? "";
  const query = q.trim();
  return queryOptions({
    queryKey: brainCaptureKeys.search(wsId, query, tag, archived, limit, kind),
    placeholderData: keepPreviousData,
    enabled: query !== "",
    queryFn: ({ signal }) =>
      api.searchWorkspaceNotes(
        { q: query, tag: tag || undefined, archived, limit, kind: kind || undefined },
        { signal },
      ),
  });
}

export function useBrainCaptures(wsId: string, status: BrainCaptureStatus | "all" = "raw") {
  return useQuery(brainCapturesOptions(wsId, status));
}

export function useBrainCapture(wsId: string, id: string) {
  return useQuery(brainCaptureOptions(wsId, id));
}

export function useNoteSearch(
  wsId: string,
  q: string,
  params?: { tag?: string; archived?: boolean; limit?: number },
) {
  return useQuery(noteSearchOptions(wsId, q, params));
}

/**
 * The inbox badge. Shares the raw list's cache entry — the server sends
 * `raw_count` with every capture list, so the badge costs no extra request
 * wherever the inbox is already mounted.
 */
export function useBrainRawCount(wsId: string) {
  return useQuery({
    ...brainCapturesOptions(wsId, "raw"),
    // The sidebar mounts outside a workspace too (the switcher, a stale tab),
    // and the endpoint resolves through the workspace-member middleware.
    enabled: wsId !== "",
    select: (data) => data.raw_count,
  });
}
