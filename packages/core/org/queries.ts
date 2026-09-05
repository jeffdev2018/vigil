import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { OrgDefinition, OrgModel, OrgStructure, OrgWriteRequest } from "../types";
import { issueKeys } from "../issues/queries";

// Executable org chart (K75): structures, templates, health, offers.

export const ORG_MODELS: OrgModel[] = ["hierarchy", "squads", "matrix", "circles", "owner_network", "taskforce", "market"];

export const orgKeys = {
  all: (wsId: string) => ["org", wsId] as const,
  list: (wsId: string) => ["org", wsId, "list"] as const,
  detail: (wsId: string, id: string) => ["org", wsId, "detail", id] as const,
  templates: (wsId: string) => ["org", wsId, "templates"] as const,
  resolve: (wsId: string, projectId: string | null) => ["org", wsId, "resolve", projectId ?? ""] as const,
  health: (wsId: string, id: string) => ["org", wsId, "health", id] as const,
  preflight: (wsId: string, id: string) => ["org", wsId, "preflight", id] as const,
  offers: (wsId: string, issueId: string) => ["org", wsId, "offers", issueId] as const,
};

export function orgListOptions(wsId: string) {
  return queryOptions({ queryKey: orgKeys.list(wsId), queryFn: () => api.listOrgStructures() });
}

export function orgDetailOptions(wsId: string, id: string) {
  return queryOptions({ queryKey: orgKeys.detail(wsId, id), queryFn: () => api.getOrgStructure(id) });
}

export function orgTemplatesOptions(wsId: string) {
  return queryOptions({ queryKey: orgKeys.templates(wsId), queryFn: () => api.listOrgTemplates(), staleTime: 5 * 60_000 });
}

export function orgResolveOptions(wsId: string, projectId: string | null) {
  return queryOptions({ queryKey: orgKeys.resolve(wsId, projectId), queryFn: () => api.resolveOrgStructure(projectId) });
}

export function orgHealthOptions(wsId: string, id: string) {
  return queryOptions({ queryKey: orgKeys.health(wsId, id), queryFn: () => api.getOrgHealth(id) });
}

export function orgPreflightOptions(wsId: string, id: string, enabled = true) {
  return queryOptions({ queryKey: orgKeys.preflight(wsId, id), queryFn: () => api.preflightOrgStructure(id), enabled });
}

export function issueOrgOffersOptions(wsId: string, issueId: string) {
  return queryOptions({ queryKey: orgKeys.offers(wsId, issueId), queryFn: () => api.listIssueOrgOffers(issueId) });
}

function useOrgMutation<V>(wsId: string, fn: (v: V) => Promise<unknown>) {
  const qc = useQueryClient();
  return useMutation({ mutationFn: fn, onSettled: () => qc.invalidateQueries({ queryKey: orgKeys.all(wsId) }) });
}

export function useCreateOrgStructure(wsId: string) {
  return useOrgMutation(wsId, (v: OrgWriteRequest) => api.createOrgStructure(v));
}

export function useUpdateOrgStructure(wsId: string) {
  return useOrgMutation(wsId, (v: { id: string; data: OrgWriteRequest }) => api.updateOrgStructure(v.id, v.data));
}

export function useSetOrgStructureStatus(wsId: string) {
  return useOrgMutation(wsId, (v: { id: string; action: "activate" | "pause" | "resume" | "dissolve"; eval_attestation?: string; reason?: string }) =>
    api.setOrgStructureStatus(v.id, v.action, { eval_attestation: v.eval_attestation, reason: v.reason }),
  );
}

export function useDeleteOrgStructure(wsId: string) {
  return useOrgMutation(wsId, (id: string) => api.deleteOrgStructure(id));
}

export function useEscalateIssue(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { issueId: string; reason: string }) => api.escalateIssue(v.issueId, v.reason),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: orgKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

export function useRouteIssueNow(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (issueId: string) => api.routeIssueNow(issueId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: orgKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

/** Mermaid `graph TD` source for a definition: units as nodes, edges labelled by kind. */
export function orgMermaid(def: OrgDefinition, pausedUnits: string[] = []): string {
  const id = (s: string) => "u_" + s.replace(/[^a-zA-Z0-9_]/g, "_");
  const esc = (s: string) => s.replace(/"/g, "'");
  const lines = ["graph TD"];
  for (const u of def.units) {
    const badge = pausedUnits.includes(u.id) ? " ⏸" : "";
    const members = u.members.length ? ` (${u.members.length})` : "";
    lines.push(`  ${id(u.id)}["${esc(u.name)}${members}${badge}"]`);
  }
  for (const e of def.edges) {
    const arrow = e.kind === "reports_to" ? "-->" : e.kind === "escalates_to" ? "==>" : "-.->";
    lines.push(`  ${id(e.from)} ${arrow}|${e.kind.replace("_", " ")}| ${id(e.to)}`);
  }
  return lines.join("\n");
}

/** Which of the seven models a structure follows, as its template name. */
export function orgModelLabel(model: OrgModel): string {
  return ({ hierarchy: "Hierarchy", squads: "Autonomous squads", matrix: "Competence × project matrix", circles: "Circles and roles", owner_network: "Owner network", taskforce: "Temporary task force", market: "Internal market" } as Record<OrgModel, string>)[model] ?? model;
}

/** Is the structure acting right now. */
export function orgIsLive(s: Pick<OrgStructure, "status">): boolean {
  return s.status === "active";
}
