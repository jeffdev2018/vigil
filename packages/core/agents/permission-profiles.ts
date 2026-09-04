import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { workspaceKeys } from "../workspace/queries";

// Permission profiles (K06): what an agent may touch when it runs. The
// workspace owns a list (five builtins seeded by the server), an agent
// carries one, an admin may override it per run.

export interface PermissionProfile {
  id: string;
  name: string;
  description: string;
  read_only: boolean;
  denied_paths: string[];
  allowed_commands: string[];
  hidden_secrets: string[];
  builtin: boolean;
}

export type PermissionProfilePatch = Partial<Omit<PermissionProfile, "id" | "builtin">>;

export const permissionProfileKeys = {
  list: (wsId: string) => ["permission-profiles", wsId] as const,
};

export function permissionProfilesOptions(wsId: string) {
  return queryOptions({ queryKey: permissionProfileKeys.list(wsId), queryFn: () => api.listPermissionProfiles(), staleTime: 60_000 });
}

export function useUpdatePermissionProfile(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; patch: PermissionProfilePatch }) => api.updatePermissionProfile(v.id, v.patch),
    onSettled: () => qc.invalidateQueries({ queryKey: permissionProfileKeys.list(wsId) }),
  });
}

export function useSetAgentPermissionProfile(wsId: string, agentId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (profileId: string | null) => api.setAgentPermissionProfile(agentId, profileId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.agent(wsId, agentId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    },
  });
}

/** Comma or newline separated text → trimmed, deduplicated list. */
export function parseList(text: string): string[] {
  const out: string[] = [];
  for (const raw of text.split(/[,\n]/)) {
    const item = raw.trim();
    if (item !== "" && !out.includes(item)) out.push(item);
  }
  return out;
}
