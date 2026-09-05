import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { AgentTask } from "../types";
import { issueKeys } from "./queries";

// Handoff packets (K17): the structured record each run leaves for the
// next hand. Immutable; a correction is a new packet.

export interface HandoffPacket {
  id: string;
  run_id: string;
  issue_id: string;
  objective: string;
  decisions: string[];
  evidence: string[];
  failed_attempts: string[];
  next_action: string;
  created_by_type: "agent" | "member" | "system";
  created_by_id: string | null;
  created_at: string;
}

export interface HandoffPacketInput {
  run_id: string;
  objective: string;
  decisions: string[];
  evidence: string[];
  failed_attempts: string[];
  next_action: string;
}

export const handoffKeys = {
  packets: (wsId: string, issueId: string) => ["handoff-packet", wsId, issueId] as const,
};

export function issueHandoffPacketsOptions(wsId: string, issueId: string) {
  return queryOptions({ queryKey: handoffKeys.packets(wsId, issueId), queryFn: () => api.listHandoffPackets(issueId), staleTime: 15_000 });
}

export function useCreateHandoffPacket(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: HandoffPacketInput) => api.createHandoffPacket(issueId, input),
    onSettled: () => qc.invalidateQueries({ queryKey: handoffKeys.packets(wsId, issueId) }),
  });
}

/** The issue's runs, newest first; the first one is the run a member hands off against. */
export function issueRunsOptions(issueId: string) {
  return queryOptions({
    queryKey: issueKeys.tasks(issueId),
    queryFn: () => api.listTasksByIssue(issueId),
    select: (tasks: AgentTask[]) => [...tasks].sort((a, b) => (b.created_at ?? "").localeCompare(a.created_at ?? "")),
  });
}

/** One item per non-empty line. */
export function splitLines(text: string): string[] {
  return text.split("\n").map((l) => l.trim()).filter((l) => l !== "");
}
