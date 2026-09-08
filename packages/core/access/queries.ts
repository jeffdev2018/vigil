import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const accessKeys = {
  sso: (wsId: string) => ["workspace-sso", wsId] as const,
  scimTokens: (wsId: string) => ["workspace-scim-tokens", wsId] as const,
  projectMembers: (wsId: string, projectId: string) => ["project", projectId, wsId, "members"] as const,
};

export function ssoConnectionOptions(wsId: string) {
  return queryOptions({ queryKey: accessKeys.sso(wsId), queryFn: () => api.getSSOConnection(wsId), enabled: !!wsId });
}

export function scimTokensOptions(wsId: string) {
  return queryOptions({ queryKey: accessKeys.scimTokens(wsId), queryFn: () => api.listScimTokens(wsId), enabled: !!wsId });
}

export function projectMembersOptions(wsId: string, projectId: string) {
  return queryOptions({ queryKey: accessKeys.projectMembers(wsId, projectId), queryFn: () => api.listProjectMembers(projectId), enabled: !!wsId && !!projectId });
}
