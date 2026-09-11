import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/** One governed MCP tool call attributed to a run. */
export interface TaskMcpCall {
  id: string;
  at: string;
  server: string;
  tool: string;
  risk: string;
  class: string;
  result: string;
  gate_id: string;
  duration_ms: number;
  flags: string[];
}

export const mcpCallKeys = {
  task: (wsId: string, taskId: string) => ["task-mcp-calls", wsId, taskId] as const,
};

export function taskMcpCallsOptions(wsId: string, taskId: string) {
  return queryOptions({
    queryKey: mcpCallKeys.task(wsId, taskId),
    queryFn: async () => (await api.listTaskMcpCalls(taskId)).calls,
    staleTime: 30_000,
  });
}

/** Map a replay mcp_call event's data bag into the panel row shape. */
export function mcpCallFromReplayData(id: string, at: string, data: Record<string, unknown>): TaskMcpCall {
  const flags = Array.isArray(data.flags) ? data.flags.filter((f): f is string => typeof f === "string") : [];
  return {
    id,
    at,
    server: typeof data.server === "string" ? data.server : "",
    tool: typeof data.tool === "string" ? data.tool : "",
    risk: typeof data.risk === "string" ? data.risk : "",
    class: typeof data.class === "string" ? data.class : "",
    result: typeof data.result === "string" ? data.result : "",
    gate_id: typeof data.gate_id === "string" ? data.gate_id : "",
    duration_ms: typeof data.duration_ms === "number" ? data.duration_ms : 0,
    flags,
  };
}
