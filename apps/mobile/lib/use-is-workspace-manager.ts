/**
 * Is the signed-in user an owner or an admin of the current workspace?
 *
 * Derived from the member list, the way web does it
 * (`useCurrentMember` in `packages/core/permissions`) and the way
 * `more/runs.tsx` already does inline — never from a client-side flag. The
 * server is still the authority (`requirePackManager` and friends answer
 * 403); this only decides whether a manager-only action is offered at all.
 */
import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { memberListOptions } from "@/data/queries/members";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";

export function useIsWorkspaceManager(): boolean {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: members = [] } = useQuery(memberListOptions(wsId));

  return useMemo(() => {
    const role = members.find((m) => m.user_id === userId)?.role;
    return role === "owner" || role === "admin";
  }, [members, userId]);
}
