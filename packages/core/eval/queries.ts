import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { EvalRun } from "./schemas";

// Eval Lab (K24). Cases and suites are static enough to fetch once; runs
// poll while one is in flight (same 10s cadence as the K39 duel) so the
// score lands without a websocket subscription.

const RUN_POLL_MS = 10_000;

export const evalKeys = {
  cases: (wsId: string) => ["eval-cases", wsId] as const,
  suites: (wsId: string) => ["eval-suites", wsId] as const,
  runs: (wsId: string) => ["eval-runs", wsId] as const,
  run: (wsId: string, runId: string) => ["eval-run", wsId, runId] as const,
};

export function evalCasesOptions(wsId: string) {
  return queryOptions({ queryKey: evalKeys.cases(wsId), queryFn: () => api.listEvalCases(wsId), enabled: !!wsId });
}

export function evalSuitesOptions(wsId: string) {
  return queryOptions({ queryKey: evalKeys.suites(wsId), queryFn: () => api.listEvalSuites(wsId), enabled: !!wsId });
}

export function evalRunsOptions(wsId: string) {
  return queryOptions({
    queryKey: evalKeys.runs(wsId),
    queryFn: () => api.listEvalRuns(wsId),
    enabled: !!wsId,
    refetchInterval: (query) =>
      (query.state.data as EvalRun[] | undefined)?.some((run) => run.status === "running") ? RUN_POLL_MS : false,
  });
}

export function evalRunOptions(wsId: string, runId: string) {
  return queryOptions({
    queryKey: evalKeys.run(wsId, runId),
    queryFn: () => api.getEvalRun(runId),
    enabled: !!runId,
    refetchInterval: (query) =>
      (query.state.data as EvalRun | null | undefined)?.status === "running" ? RUN_POLL_MS : false,
  });
}
