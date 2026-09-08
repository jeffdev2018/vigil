"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { assigneeSuggestionOptions, competencyDomainLabel, competencyRate, estimateCostRange, estimateDurationRange, issueEstimateOptions } from "@multica/core/agents/competency";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

/**
 * Learned competency (K43): one line per agent with a history on this
 * issue's domain — "82% (14 issues)" — greyed while the sample is small.
 * A signal beside K27/K33, never an assignment; hidden without any data.
 *
 * The what-if estimate (K44) rides along on its own query so the history
 * renders without waiting for it: "8–15 min · $0.30–0.50", or a plain
 * admission that the history is too thin to say — never a made-up number.
 */
export function CompetencySuggestion({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data } = useQuery(assigneeSuggestionOptions(wsId, issueId));
  const candidateIds = data?.candidates.map((c) => c.agent_id) ?? [];
  const { data: estimate, isPending: estimatePending } = useQuery({ ...issueEstimateOptions(wsId, issueId, candidateIds), enabled: candidateIds.length > 0 });
  if (!data || data.candidates.length === 0) return null;
  const estimates = new Map((estimate?.candidates ?? []).map((c) => [c.agent_id, c]));
  return (
    <div data-testid="competency-suggestion" className="flex flex-col gap-1 rounded-md border border-border p-2 text-caption">
      <div className="flex items-center gap-2 font-medium">
        <span>{t(($) => $.competency.section)}</span>
        <span className="font-mono font-normal text-muted-foreground">{t(($) => $.competency.domain, { domain: competencyDomainLabel(data.domain_key) })}</span>
      </div>
      <ul className="flex flex-col gap-0.5">
        {data.candidates.map((c) => {
          const est = estimates.get(c.agent_id);
          const duration = est ? estimateDurationRange(est.duration_range_low_seconds, est.duration_range_high_seconds) : "";
          const cost = est ? estimateCostRange(est.cost_range_low_usd_ticks, est.cost_range_high_usd_ticks) : "";
          return (
            <li key={c.agent_id} data-testid="competency-candidate" data-reliable={c.reliable} className={cn("flex flex-wrap items-center gap-x-2", !c.reliable && "text-muted-foreground")}>
              <span>{t(($) => $.competency.row, { name: c.agent_name || c.agent_id.slice(0, 8), rate: competencyRate(c.score), count: c.total_count })}</span>
              {(c.duel_wins > 0 || c.duel_losses > 0) && <span className="text-muted-foreground">{t(($) => $.competency.duels, { wins: c.duel_wins, losses: c.duel_losses })}</span>}
              {!c.reliable && <span className="italic">{t(($) => $.competency.low_sample, { count: c.sample_size, min: data.min_sample })}</span>}
              {estimatePending && <span data-testid="estimate-loading" className="inline-block h-3 w-24 animate-pulse rounded bg-muted" />}
              {!estimatePending &&
                (duration && cost ? (
                  <span data-testid="estimate" className="text-muted-foreground">
                    {t(($) => $.competency.estimate_range, { duration, cost })}
                  </span>
                ) : (
                  <span data-testid="estimate-empty" className="italic text-muted-foreground">
                    {t(($) => $.competency.estimate_empty)}
                  </span>
                ))}
              {est?.exceeds_budget === true && (
                <span data-testid="estimate-over-budget" className="rounded bg-warning/15 px-1 text-warning">
                  {t(($) => $.competency.estimate_over_budget)}
                </span>
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );
}
