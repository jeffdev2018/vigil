import { keepPreviousData, queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const brainKeys = {
  all: (wsId: string) => ["brain", wsId] as const,
  list: (wsId: string, search: string, tag: string, archived: boolean) =>
    [...brainKeys.all(wsId), "list", search, tag, archived] as const,
  detail: (wsId: string, id: string) => [...brainKeys.all(wsId), "detail", id] as const,
};

/**
 * The Brain listing. Search and tag are part of the key because the server
 * owns both filters (full-text search runs on the GIN index, not in the
 * browser), so each combination is its own cache entry.
 */
export function brainNotesOptions(
  wsId: string,
  params?: { search?: string; tag?: string; archived?: boolean },
) {
  const search = params?.search ?? "";
  const tag = params?.tag ?? "";
  const archived = params?.archived === true;
  return queryOptions({
    queryKey: brainKeys.list(wsId, search, tag, archived),
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
