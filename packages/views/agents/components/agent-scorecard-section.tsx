"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentScorecardOptions, scorecardRate } from "@multica/core/agents/queries";
import type { ScorecardTotals } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { formatUsd } from "../../runtimes/utils";
import { useT } from "../../i18n";

/**
 * Scorecard (K25): the agent's last 30 days against the 30 before —
 * acceptance, failures, reopenings, resolution without a human stepping
 * in, mean cost. Each metric stays on its own; a small sample is said so.
 */
export function AgentScorecardSection({ agentId }: { agentId: string }) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const { data } = useQuery(agentScorecardOptions(wsId, agentId, 30));
  if (!data) return null;
  const { totals, previous } = data;
  if (totals.runs_total === 0) {
    return (
      <section data-testid="agent-scorecard" data-empty="true" className="mt-4 text-caption text-muted-foreground">
        <h3 className="mb-1 font-medium text-foreground">{t(($) => $.scorecard.title)}</h3>
        {t(($) => $.scorecard.empty)}
      </section>
    );
  }
  return (
    <section data-testid="agent-scorecard" className="mt-4 text-caption">
      <h3 className="mb-1 font-medium">
        {t(($) => $.scorecard.title)}
        {totals.low_sample && <span className="ml-2 font-normal text-muted-foreground">{t(($) => $.scorecard.low_sample, { count: totals.runs_total })}</span>}
      </h3>
      <dl className="grid grid-cols-2 gap-x-4 gap-y-1">
        <Metric label={t(($) => $.scorecard.acceptance)} now={scorecardRate(totals.runs_accepted, totals.runs_total)} before={scorecardRate(previous.runs_accepted, previous.runs_total)} good="up" />
        <Metric label={t(($) => $.scorecard.failure)} now={scorecardRate(totals.runs_failed, totals.runs_total)} before={scorecardRate(previous.runs_failed, previous.runs_total)} good="down" />
        <Metric label={t(($) => $.scorecard.reopened)} now={scorecardRate(totals.runs_reopened, totals.runs_total)} before={scorecardRate(previous.runs_reopened, previous.runs_total)} good="down" />
        <Metric label={t(($) => $.scorecard.no_intervention)} now={scorecardRate(totals.runs_no_intervention, totals.runs_total)} before={scorecardRate(previous.runs_no_intervention, previous.runs_total)} good="up" />
        <div className="contents">
          <dt className="text-muted-foreground">{t(($) => $.scorecard.mean_cost)}</dt>
          <dd className="tabular-nums" data-testid="scorecard-cost">{meanCost(totals)}</dd>
        </div>
      </dl>
    </section>
  );
}

function meanCost(t: ScorecardTotals): string {
  return t.runs_total > 0 ? formatUsd(t.cost_usd_ticks_total / 1e10 / t.runs_total) : "—";
}

function Metric({ label, now, before, good }: { label: string; now: number | null; before: number | null; good: "up" | "down" }) {
  const delta = now !== null && before !== null ? now - before : null;
  const improving = delta !== null && delta !== 0 && (good === "up" ? delta > 0 : delta < 0);
  return (
    <div className="contents">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="tabular-nums" data-testid="scorecard-metric">
        {now === null ? "—" : `${now}%`}
        {delta !== null && delta !== 0 && (
          <span className={cn("ml-1", improving ? "text-success" : "text-warning")}>
            {delta > 0 ? "+" : ""}
            {delta}
          </span>
        )}
      </dd>
    </div>
  );
}
