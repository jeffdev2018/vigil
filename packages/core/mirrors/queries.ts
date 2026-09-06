import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const mirrorKeys = {
  links: (wsId: string, projectId: string) => ["project", projectId, wsId, "mirror-links"] as const,
  issueMirrors: (wsId: string, issueId: string) => ["issue", issueId, wsId, "mirrors"] as const,
};

export function mirrorLinksOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: mirrorKeys.links(wsId, projectId),
    queryFn: () => api.listMirrorLinks(projectId),
    enabled: !!wsId && !!projectId,
  });
}

export function issueMirrorsOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: mirrorKeys.issueMirrors(wsId, issueId),
    queryFn: () => api.getIssueMirrors(issueId),
    enabled: !!wsId && !!issueId,
  });
}
