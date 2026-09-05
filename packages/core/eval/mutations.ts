import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { evalKeys } from "./queries";
import type { CreateEvalSuiteRequest, RunEvalSuiteRequest } from "./schemas";

/** Promoting a resolved issue mints a new eval case for the whole workspace. */
export function usePromoteIssueToEvalCase(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.promoteIssueToEvalCase(issueId),
    onSettled: () => qc.invalidateQueries({ queryKey: evalKeys.cases(wsId) }),
  });
}

export function useCreateEvalSuite(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateEvalSuiteRequest) => api.createEvalSuite(wsId, input),
    onSettled: () => qc.invalidateQueries({ queryKey: evalKeys.suites(wsId) }),
  });
}

/** Launching a run changes both the run list and the suite's "last run" column. */
export function useRunEvalSuite(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ suiteId, ...input }: RunEvalSuiteRequest & { suiteId: string }) => api.runEvalSuite(suiteId, input),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: evalKeys.runs(wsId) });
      qc.invalidateQueries({ queryKey: evalKeys.suites(wsId) });
    },
  });
}
