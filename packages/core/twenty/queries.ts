import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

// Settings-page shaped: fetched when the card opens, invalidated by the
// mutations, never polled.

export const twentyKeys = {
  status: (wsId: string) => ["twenty-status", wsId] as const,
  members: (wsId: string) => ["twenty-members", wsId] as const,
};

export function twentyStatusOptions(wsId: string) {
  return queryOptions({
    queryKey: twentyKeys.status(wsId),
    queryFn: () => api.getTwentyStatus(),
    enabled: !!wsId,
  });
}

export function twentyMembersOptions(wsId: string) {
  return queryOptions({
    queryKey: twentyKeys.members(wsId),
    queryFn: () => api.listTwentyMembers(),
    enabled: !!wsId,
  });
}
