import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { issueKeys } from "./queries";

// Pause, steer, resume (K19): the run a human may correct without
// restarting it. Pause takes effect at the daemon's next safe boundary;
// instructions are typed messages; resume continues the same session.

export interface RunControlState {
  task_id: string;
  status: string;
  pause_pending: boolean;
  instructions: string[];
  resumed_by_task_id: string | null;
}

export const runControlKeys = {
  state: (wsId: string, issueId: string) => ["run-state", wsId, issueId] as const,
};

export function issueRunStateOptions(wsId: string, issueId: string) {
  return queryOptions({ queryKey: runControlKeys.state(wsId, issueId), queryFn: () => api.getRunState(issueId), refetchInterval: 5_000 });
}

function useRunControlMutation<TArg>(wsId: string, issueId: string, fn: (arg: TArg) => Promise<RunControlState | null>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSettled: () => {
      qc.invalidateQueries({ queryKey: runControlKeys.state(wsId, issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.tasks(issueId) });
    },
  });
}

export function usePauseRun(wsId: string, issueId: string) {
  return useRunControlMutation<void>(wsId, issueId, () => api.pauseRun(issueId));
}

export function useSteerRun(wsId: string, issueId: string) {
  return useRunControlMutation<string>(wsId, issueId, (instruction) => api.steerRun(issueId, instruction));
}

export function useResumeRun(wsId: string, issueId: string) {
  return useRunControlMutation<void>(wsId, issueId, () => api.resumeRun(issueId));
}
