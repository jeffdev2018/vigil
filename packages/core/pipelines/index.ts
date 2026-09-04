import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { issueKeys } from "../issues/queries";

// Pipelines (K37): ordered stages an issue moves through, each routed to
// an agent or a squad, with an optional human gate before it.

export interface PipelineStage {
  id: string;
  position: number;
  name: string;
  executor_type: "agent" | "squad";
  executor_id: string;
  requires_human_gate: boolean;
}

export interface PipelineStageInput {
  name: string;
  executor_type: "agent" | "squad";
  executor_id: string;
  requires_human_gate: boolean;
}

export interface Pipeline {
  id: string;
  name: string;
  stages: PipelineStage[];
  open_runs: number;
  created_at: string;
}

export interface PipelineInput {
  name: string;
  stages?: PipelineStageInput[];
}

export interface PipelineRun {
  id: string;
  pipeline_id: string;
  pipeline_name: string;
  issue_id: string;
  status: "active" | "paused" | "completed" | "cancelled";
  current_stage_id: string | null;
  current_index: number;
  gate_decision_id: string | null;
  last_error: string | null;
  stages: PipelineStage[];
  started_at: string;
  completed_at: string | null;
}

export const pipelineKeys = {
  list: (wsId: string) => ["pipeline", wsId] as const,
  run: (wsId: string, issueId: string) => ["pipelineRun", wsId, issueId] as const,
  squads: (wsId: string) => ["squads", wsId, "pipeline-executors"] as const,
};

export function pipelinesOptions(wsId: string) {
  return queryOptions({ queryKey: pipelineKeys.list(wsId), queryFn: () => api.listPipelines() });
}

export function issuePipelineRunOptions(wsId: string, issueId: string) {
  return queryOptions({ queryKey: pipelineKeys.run(wsId, issueId), queryFn: () => api.getIssuePipelineRun(issueId), refetchInterval: 15_000 });
}

export function pipelineSquadsOptions(wsId: string) {
  return queryOptions({ queryKey: pipelineKeys.squads(wsId), queryFn: () => api.listSquads(), staleTime: 60_000 });
}

export function useSavePipeline(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id?: string; input: PipelineInput }) => (v.id ? api.updatePipeline(v.id, v.input) : api.createPipeline(v.input)),
    onSettled: () => qc.invalidateQueries({ queryKey: pipelineKeys.list(wsId) }),
  });
}

export function useDeletePipeline(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deletePipeline(id),
    onSettled: () => qc.invalidateQueries({ queryKey: pipelineKeys.list(wsId) }),
  });
}

function useRunMutation<TArg>(wsId: string, issueId: string, fn: (arg: TArg) => Promise<PipelineRun | null>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSettled: () => {
      qc.invalidateQueries({ queryKey: pipelineKeys.run(wsId, issueId) });
      qc.invalidateQueries({ queryKey: pipelineKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.tasks(issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.decisions(wsId, issueId) });
    },
  });
}

export function useStartPipelineRun(wsId: string, issueId: string) {
  return useRunMutation<string>(wsId, issueId, (pipelineId) => api.startPipelineRun(issueId, pipelineId));
}

export function useAdvancePipelineRun(wsId: string, issueId: string) {
  return useRunMutation<string>(wsId, issueId, (runId) => api.advancePipelineRun(runId));
}

export function useCancelPipelineRun(wsId: string, issueId: string) {
  return useRunMutation<string>(wsId, issueId, (runId) => api.cancelPipelineRun(runId));
}

/** Where the cursor stands, for a progress bar: done / current / todo per stage. */
export function stageStates(run: PipelineRun): ("done" | "current" | "gate" | "todo")[] {
  return run.stages.map((_, i) => {
    if (run.status === "completed" || i < run.current_index) return "done";
    if (i === run.current_index) return run.status === "paused" && run.gate_decision_id ? "gate" : "current";
    return "todo";
  });
}
