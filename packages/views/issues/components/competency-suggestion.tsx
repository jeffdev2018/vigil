"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { assigneeSuggestionOptions, competencyDomainLabel, competencyRate } from "@multica/core/agents/competency";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

/**
 * Learned competency (K43): one line per agent with a history on this
 * issue's domain — "82% (14 issues)" — greyed while the sample is small.
 * A signal beside K27/K33, never an assignment; hidden without any data.
 */
export function CompetencySuggestion({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data } = useQuery(assigneeSuggestionOptions(wsId, issueId));
  if (!data || data.candidates.length === 0) return null;
  return (
    <div data-testid="competency-suggestion" className="flex flex-col gap-1 rounded-md border border-border p-2 text-caption">
      <div className="flex items-center gap-2 font-medium">
        <span>{t(($) => $.competency.section)}</span>
        <span className="font-mono font-normal text-muted-foreground">{t(($) => $.competency.domain, { domain: competencyDomainLabel(data.domain_key) })}</span>
      </div>
      <ul className="flex flex-col gap-0.5">
        {data.candidates.map((c) => (
          <li key={c.agent_id} data-testid="competency-candidate" data-reliable={c.reliable} className={cn("flex flex-wrap items-center gap-x-2", !c.reliable && "text-muted-foreground")}>
            <span>{t(($) => $.competency.row, { name: c.agent_name || c.agent_id.slice(0, 8), rate: competencyRate(c.score), count: c.total_count })}</span>
            {(c.duel_wins > 0 || c.duel_losses > 0) && <span className="text-muted-foreground">{t(($) => $.competency.duels, { wins: c.duel_wins, losses: c.duel_losses })}</span>}
            {!c.reliable && <span className="italic">{t(($) => $.competency.low_sample, { count: c.sample_size, min: data.min_sample })}</span>}
          </li>
        ))}
      </ul>
    </div>
  );
}
