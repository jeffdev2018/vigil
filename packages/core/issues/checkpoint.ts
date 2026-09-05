import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

// Checkpoints (K20): the latest run's resume point and how many times an
// interruption resumed it. The resume itself is automatic.

export interface RunCheckpointStatus {
  task_id: string;
  status: string;
  failure_reason: string;
  last_checkpoint_seq: number | null;
  checkpointed_at: string | null;
  attempts: number;
  max_attempts: number;
  resumed_from_task_id: string | null;
  exhausted: boolean;
}

export const checkpointKeys = {
  status: (wsId: string, issueId: string) => ["run-checkpoint", wsId, issueId] as const,
};

export function issueRunCheckpointOptions(wsId: string, issueId: string) {
  return queryOptions({ queryKey: checkpointKeys.status(wsId, issueId), queryFn: () => api.getRunCheckpointStatus(issueId), staleTime: 15_000 });
}
