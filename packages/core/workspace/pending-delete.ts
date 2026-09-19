/**
 * Registry of workspace IDs whose DELETE was initiated by THIS client.
 *
 * Marked in useDeleteWorkspace.onMutate. The realtime `workspace:deleted`
 * handler checks it and no-ops for self-initiated deletes: the mutation flow
 * owns storage cleanup and navigation (both run after the DELETE resolves),
 * and letting the handler react too would race that flow's navigation with
 * its own full-page relocate.
 *
 * Lifted only when the DELETE fails — the workspace still exists, so a later
 * external delete of the same ID must be handled normally. On success the ID
 * is gone for good, and keeping the mark suppresses the WS echo of our own
 * delete no matter when it arrives.
 *
 * Module scope rather than React state because the realtime handler runs
 * outside the component tree. Per-tab by construction: other tabs/devices
 * have an empty registry, so their handlers process the event normally.
 */
const pendingDeletes = new Set<string>();

export function markWorkspaceDeletePending(workspaceId: string) {
  pendingDeletes.add(workspaceId);
}

export function unmarkWorkspaceDeletePending(workspaceId: string) {
  pendingDeletes.delete(workspaceId);
}

/** True if this client initiated a DELETE for the workspace. */
export function isWorkspaceDeletePending(workspaceId: string): boolean {
  return pendingDeletes.has(workspaceId);
}

/**
 * Registry of workspace IDs this client is LEAVING.
 *
 * Marked in useLeaveWorkspace.onMutate. Same purpose as the delete registry
 * above, for the other event: the server broadcasts `member:removed` for our
 * own leave, and the realtime handler would answer it with a full-page
 * relocate that races the leave flow's navigation and its own
 * `invalidateQueries` refetch (whoever loses gets a CancelledError). The flow
 * owns navigation and storage cleanup; the handler only serves removals
 * decided elsewhere.
 *
 * Unlike a delete, the workspace still exists after we leave it, so the mark
 * is lifted once the flow is done with it — on failure because we are still a
 * member, and on success right after navigating, by which point the
 * workspace-context singleton is null and the handler no-ops on its own. A
 * later re-join followed by a real removal is then handled normally.
 */
const pendingLeaves = new Set<string>();

export function markWorkspaceLeavePending(workspaceId: string) {
  pendingLeaves.add(workspaceId);
}

export function unmarkWorkspaceLeavePending(workspaceId: string) {
  pendingLeaves.delete(workspaceId);
}

/** True if this client initiated a leave for the workspace. */
export function isWorkspaceLeavePending(workspaceId: string): boolean {
  return pendingLeaves.has(workspaceId);
}
