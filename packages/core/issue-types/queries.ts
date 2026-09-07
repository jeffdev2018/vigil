import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { IssueTypeEntry } from "../types";

/**
 * The workspace work item type catalogue (F30 / JEF-34).
 *
 * A type answers "what IS this issue" — bug, story, epic, task, or a custom
 * one. It carries no platform behavior, so this catalogue only ever has to
 * answer presentation questions: given the key stored on an issue, what is its
 * label, colour and icon, and is it still assignable?
 */

export const issueTypeKeys = {
  all: (wsId: string) => ["issue-types", wsId] as const,
  list: (wsId: string) => [...issueTypeKeys.all(wsId), "list"] as const,
};

export function issueTypeListOptions(wsId: string) {
  return queryOptions({
    queryKey: issueTypeKeys.list(wsId),
    // ARCHIVED entries included on purpose. Archiving retires a type from
    // future assignment but leaves the issues already on it, and those issues
    // must keep their real name, colour and icon. Dropping archived rows here
    // would degrade them to a raw key. Pickers use `activeTypes`.
    queryFn: () => api.listIssueTypes(true),
    select: (data) => data.types,
    // The catalogue changes only when an admin edits it, which is rare.
    staleTime: 5 * 60_000,
  });
}

/**
 * A resolved view over the catalogue. Every lookup falls back to something
 * renderable, because an issue can legitimately carry a type this client has
 * not heard of: one created moments ago in another session, or one whose
 * catalogue fetch has not landed yet.
 */
export interface IssueTypeCatalog {
  /** Every type in display order, ARCHIVED INCLUDED. */
  types: IssueTypeEntry[];
  /** Assignable types — `types` minus archived ones. */
  activeTypes: IssueTypeEntry[];
  /** Catalogue entry for a key, when this client knows it. */
  entryOf: (key: string | null | undefined) => IssueTypeEntry | undefined;
  /** Human label for a key; falls back to the raw key so an unknown type still
   *  reads as something rather than vanishing. */
  labelOf: (key: string | null | undefined) => string;
  /** "#rrggbb" for a key, or null when this client cannot resolve it. */
  colorOf: (key: string | null | undefined) => string | null;
  /** True once the catalogue has loaded; false while it is still in flight. */
  isLoaded: boolean;
  isPending: boolean;
  /** The request failed AND there is no usable snapshot behind it. A background
   *  refetch failing over a cached catalogue is a stale-data situation, not a
   *  blocking one. */
  isError: boolean;
  retry: () => void;
}

/** The server's ordering, mirrored for client-side re-sorts: position, then key
 *  as a stable tiebreak. An optimistic reorder has to re-sort with this or the
 *  new positions land in the cache while the list still renders in the old
 *  order. */
export function compareIssueTypeEntries(a: IssueTypeEntry, b: IssueTypeEntry): number {
  if (a.position !== b.position) return a.position - b.position;
  return a.key.localeCompare(b.key);
}

/**
 * Builds the resolved catalogue from a raw entry list. Pure, so non-React
 * callers can use it with a list they already hold.
 */
export function buildIssueTypeCatalog(
  entries: IssueTypeEntry[] | undefined,
  status: { isPending?: boolean; isError?: boolean; retry?: () => void } = {},
): IssueTypeCatalog {
  const list = entries ?? [];
  const byKey = new Map(list.map((e) => [e.key, e]));
  const entryOf = (key: string | null | undefined) => (key ? byKey.get(key) : undefined);
  return {
    types: list,
    activeTypes: list.filter((e) => !e.archived_at),
    entryOf,
    labelOf: (key) => {
      if (!key) return "";
      return entryOf(key)?.name ?? key;
    },
    colorOf: (key) => entryOf(key)?.color ?? null,
    isLoaded: entries !== undefined,
    isPending: status.isPending ?? entries === undefined,
    isError: (status.isError ?? false) && entries === undefined,
    retry: status.retry ?? (() => {}),
  };
}
