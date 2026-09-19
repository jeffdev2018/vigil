"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentCostEstimateOptions } from "@multica/core/agents";
import { runtimeDisplayLabel, runtimeListOptions } from "@multica/core/runtimes";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useT } from "../../i18n";
import { formatUsd } from "../../runtimes/utils";

const TICKS_PER_USD = 1e10;

/**
 * "Runs on <runtime> · ≈ $x per run" — the line every surface that starts an
 * agent run shows BEFORE the user commits (create with an agent, assign on
 * create, @mention in a comment, a chat message). The cost is the agent's
 * recent average from the server (GET /api/agents/{id}/cost-estimate, priced
 * like budget settlement); without a priced history it says "cost unknown",
 * never a zero.
 */
export function useAgentRunDetails(agentId: string | null | undefined): string | null {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const estimate = useQuery({ ...agentCostEstimateOptions(wsId, agentId ?? ""), enabled: Boolean(wsId && agentId) });
  if (!agentId) return null;
  const agent = agents.find((a) => a.id === agentId);
  const runtime = agent?.runtime_id ? runtimes.find((r) => r.id === agent.runtime_id) : undefined;
  const runtimeLabel = runtime ? runtimeDisplayLabel(runtime) : t(($) => $.run_details.runtime_unknown);
  const ticks = estimate.data?.avg_cost_usd_ticks;
  const cost = typeof ticks === "number"
    ? t(($) => $.run_details.cost_average, { cost: formatUsd(ticks / TICKS_PER_USD), count: estimate.data?.sample_runs ?? 0 })
    : estimate.isPending
      ? t(($) => $.run_details.cost_loading)
      : t(($) => $.run_details.cost_unknown);
  return agent?.runtime_routing === "auto"
    ? t(($) => $.run_details.details_auto, { runtime: runtimeLabel, cost })
    : t(($) => $.run_details.details, { runtime: runtimeLabel, cost });
}

export function AgentRunDetails({ agentId, className }: { agentId: string; className?: string }) {
  const details = useAgentRunDetails(agentId);
  if (!details) return null;
  return <span data-testid="agent-run-details" className={className}>{details}</span>;
}
