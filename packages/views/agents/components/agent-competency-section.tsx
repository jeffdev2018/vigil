"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentCompetencyOptions, competencyDomainLabel, competencyRate } from "@multica/core/agents/competency";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

/**
 * Learned competency (K43): the agent's success rate per domain, most
 * data first; duel wins and losses shown apart; small samples greyed and
 * said so instead of a misleading percentage.
 */
export function AgentCompetencySection({ agentId }: { agentId: string }) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const { data } = useQuery(agentCompetencyOptions(wsId, agentId));
  if (!data) return null;
  return (
    <section data-testid="agent-competency" data-empty={data.rows.length === 0} className="mt-4 text-caption">
      <h3 className="mb-1 font-medium">{t(($) => $.competency.title)}</h3>
      {data.rows.length === 0 ? (
        <p className="text-muted-foreground">{t(($) => $.competency.empty)}</p>
      ) : (
        <ul className="flex flex-col gap-0.5">
          {data.rows.map((r) => (
            <li key={r.domain_key} data-testid="agent-competency-row" data-reliable={r.reliable} className={cn("flex flex-wrap items-center gap-x-2", !r.reliable && "text-muted-foreground")}>
              <span className="font-mono">{competencyDomainLabel(r.domain_key)}</span>
              <span>{r.reliable ? t(($) => $.competency.rate, { rate: competencyRate(r.score) }) : t(($) => $.competency.low_sample, { count: r.sample_size, min: data.min_sample })}</span>
              <span className="text-muted-foreground">{t(($) => $.competency.sample, { count: r.total_count })}</span>
              {(r.duel_wins > 0 || r.duel_losses > 0) && <span className="text-muted-foreground">{t(($) => $.competency.duels, { wins: r.duel_wins, losses: r.duel_losses })}</span>}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
