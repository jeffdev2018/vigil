import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { evalKeys } from "./queries";
import type { BenchmarkPolicySearchRequest, CreateEvalSuiteRequest, RunBenchmarkRequest, RunEvalSuiteRequest } from "./schemas";

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

/**
 * Internal benchmark harness (JEF-276). One benchmark creates an eval run per
 * candidate, so it moves the benchmark list, the ordinary run list and every
 * suite's "last run" column at once.
 */
export function useRunBenchmark(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ suiteId, ...input }: RunBenchmarkRequest & { suiteId: string }) => api.runBenchmark(suiteId, input),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: evalKeys.benchmarks(wsId) });
      qc.invalidateQueries({ queryKey: evalKeys.runs(wsId) });
      qc.invalidateQueries({ queryKey: evalKeys.suites(wsId) });
    },
  });
}

/**
 * Replays the router's scoring offline over the named benchmark runs. Nothing
 * is applied server-side — it only reports — so there is no cache to touch.
 */
export function useBenchmarkPolicySearch(wsId: string) {
  return useMutation({
    mutationFn: (input: BenchmarkPolicySearchRequest) => api.benchmarkPolicySearch(wsId, input),
  });
}
