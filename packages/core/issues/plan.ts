import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { IssuePlan, IssuePlanStep, PlanFinding, PlanVerification } from "../types";
import { issueKeys } from "./queries";

export function issuePlanOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: issueKeys.plan(wsId, issueId),
    queryFn: ({ signal }) => api.getIssuePlan(issueId, { signal }),
  });
}

export function planVerificationsOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: issueKeys.planVerifications(wsId, issueId),
    queryFn: ({ signal }) => api.listPlanVerifications(issueId, { signal }),
  });
}

// Plan Gate (K11).

/**
 * Stage per step: 1 + the deepest stage among the steps it comes after.
 * Mirrors the server's planStages; an unknown or cyclic reference yields
 * stage 1 for that step here, the server is what refuses it.
 */
export function planStepStages(steps: IssuePlanStep[]): Map<string, number> {
  const byId = new Map(steps.map((s) => [s.id, s]));
  const stages = new Map<string, number>();
  const visiting = new Set<string>();
  const stageOf = (id: string): number => {
    const known = stages.get(id);
    if (known !== undefined) return known;
    if (visiting.has(id)) return 1;
    visiting.add(id);
    let stage = 1;
    for (const dep of byId.get(id)?.after ?? []) {
      if (dep !== id && byId.has(dep)) stage = Math.max(stage, stageOf(dep) + 1);
    }
    visiting.delete(id);
    stages.set(id, stage);
    return stage;
  };
  for (const s of steps) stageOf(s.id);
  return stages;
}

export function isPlanMaterialized(plan: Pick<IssuePlan, "materialized_at">): boolean {
  return typeof plan.materialized_at === "string" && plan.materialized_at !== "";
}

export function useMaterializeIssuePlan(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { issueId: string; version: number }) => api.materializeIssuePlan(v.issueId, v.version),
    onSettled: (_data, _err, v) => {
      qc.invalidateQueries({ queryKey: issueKeys.plan(wsId, v.issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.decisions(wsId, v.issueId) });
      // The sub-issues land in lists and the parent's children.
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

export function useSetIssuePlan(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { issueId: string; content: string; steps?: IssuePlanStep[] }) =>
      api.setIssuePlan(v.issueId, { content: v.content, steps: v.steps }),
    onSettled: (_data, _err, v) => {
      qc.invalidateQueries({ queryKey: issueKeys.plan(wsId, v.issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.planVerifications(wsId, v.issueId) });
    },
  });
}

export const PLAN_FINDING_SEVERITIES = ["critical", "major", "minor", "outdated"] as const;

/** Sort weight: critical first, unknown severities after the known ones. */
export function planSeverityRank(severity: string): number {
  const i = (PLAN_FINDING_SEVERITIES as readonly string[]).indexOf(severity.toLowerCase());
  return i === -1 ? PLAN_FINDING_SEVERITIES.length : i;
}

export function sortPlanFindings(findings: PlanFinding[]): PlanFinding[] {
  return findings.slice().sort((a, b) => planSeverityRank(a.severity) - planSeverityRank(b.severity));
}

/** The verification a reader cares about: the newest one. */
export function latestPlanVerification(list: PlanVerification[]): PlanVerification | null {
  if (list.length === 0) return null;
  return list.slice().sort((a, b) => b.created_at.localeCompare(a.created_at))[0] ?? null;
}

/** True only when a reported verification carries a critical finding. */
export function planVerificationBlocksDone(v: PlanVerification | null): boolean {
  return v !== null && v.state === "reported" && v.critical_count > 0;
}
