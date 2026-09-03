import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { IssuePlanStep, PlanFinding, PlanVerification } from "../types";
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
