import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { issueKeys } from "./queries";

// Agent duel (K39): two independent runs of one issue, the arbiter's
// scores on top of measured cost and duration, and the human's verdict.

export type DuelWinner = "a" | "b" | "tie";

export interface AgentDuelSide {
  agent_id: string;
  task_id: string;
  task_status: string;
  outcome: "completed" | "failed" | null;
  cost_usd_ticks: number;
  duration_seconds: number;
  tool_calls: number;
  quality_score: number | null;
  summary: string;
}

export interface AgentDuel {
  id: string;
  issue_id: string;
  status: "running" | "verdict_ready" | "confirmed" | "inconclusive";
  a: AgentDuelSide;
  b: AgentDuelSide;
  arbiter_winner: DuelWinner | null;
  reasoning: string;
  arbiter_error: string | null;
  winner: DuelWinner | null;
  confirmed_by: string | null;
  confirmed_at: string | null;
  created_at: string;
  settled_at: string | null;
}

export interface DuelInput {
  agent_a_id: string;
  agent_b_id: string;
}

const TICKS_PER_USD = 1e10;

/** "$0.12" from cost ticks; sub-cent amounts keep four decimals. */
export function duelCostUsd(ticks: number): string {
  const usd = ticks / TICKS_PER_USD;
  return usd > 0 && usd < 0.01 ? `$${usd.toFixed(4)}` : `$${usd.toFixed(2)}`;
}

/** "1m 30s" from seconds. */
export function duelDuration(seconds: number): string {
  const s = Math.max(0, Math.round(seconds));
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
}

export const duelKeys = {
  issue: (wsId: string, issueId: string) => ["duel", wsId, issueId] as const,
};

export function issueDuelOptions(wsId: string, issueId: string) {
  return queryOptions({ queryKey: duelKeys.issue(wsId, issueId), queryFn: () => api.getIssueDuel(issueId), refetchInterval: 10_000 });
}

export function useStartDuel(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: DuelInput) => api.startDuel(issueId, input),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: duelKeys.issue(wsId, issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.tasks(issueId) });
    },
  });
}

export function useConfirmDuel(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ duelId, winner }: { duelId: string; winner: DuelWinner }) => api.confirmDuel(duelId, winner),
    onSettled: () => qc.invalidateQueries({ queryKey: duelKeys.issue(wsId, issueId) }),
  });
}
