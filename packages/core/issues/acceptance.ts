import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { AcceptanceCriterion } from "../types";
import { issueKeys } from "./queries";

// Outcome Contract (K12): the criteria list is server state; the proof state
// is the server's verdict, never derived here.

export const ACCEPTANCE_PROOF_TYPES = ["test", "file", "screenshot", "url", "human_validation"] as const;

export function acceptanceCriteriaOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: issueKeys.acceptance(wsId, issueId),
    queryFn: ({ signal }) => api.listAcceptanceCriteria(issueId, { signal }),
  });
}

function useCriteriaMutation<V extends { issueId: string }>(wsId: string, fn: (v: V) => Promise<AcceptanceCriterion[]>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: (criteria, v) => qc.setQueryData(issueKeys.acceptance(wsId, v.issueId), criteria),
    onSettled: (_data, _err, v) => qc.invalidateQueries({ queryKey: issueKeys.acceptance(wsId, v.issueId) }),
  });
}

export function useSetAcceptanceCriteria(wsId: string) {
  return useCriteriaMutation(wsId, (v: { issueId: string; criteria: { id?: string; text: string }[] }) =>
    api.setAcceptanceCriteria(v.issueId, v.criteria),
  );
}

export function useProveAcceptanceCriterion(wsId: string) {
  return useCriteriaMutation(wsId, (v: { issueId: string; criterionId: string; proof_type: string; proof_ref?: string }) =>
    api.proveAcceptanceCriterion(v.issueId, v.criterionId, { proof_type: v.proof_type, proof_ref: v.proof_ref }),
  );
}

export function isCriterionSatisfied(c: Pick<AcceptanceCriterion, "proof_state">): boolean {
  return c.proof_state === "satisfied";
}

export function unsatisfiedCriteria(list: AcceptanceCriterion[]): AcceptanceCriterion[] {
  return list.filter((c) => !isCriterionSatisfied(c));
}
