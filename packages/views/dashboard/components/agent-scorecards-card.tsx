"use client";

import { useQuery } from "@tanstack/react-query";
import { scorecardRate, workspaceScorecardsOptions } from "@multica/core/agents/queries";
import { agentListOptions } from "@multica/core/workspace/queries";
import { cn } from "@multica/ui/lib/utils";
import { formatUsd } from "../../runtimes/utils";
import { useT } from "../../i18n";

/**
 * Scorecards (K25): every agent of the workspace over the period, one row
 * each, the metrics side by side. Rows with too few runs are greyed, not
 * hidden: an agent that barely ran is a fact too.
 *
 * Scope note: the rows come from the agent_scorecard_daily rollup, which has
 * no project dimension and buckets by UTC day. So unlike its neighbours this
 * card follows neither the page's project filter nor its display timezone,
 * and says so in the header rather than showing filtered-looking numbers that
 * are not. Narrowing it would mean re-deriving the metrics from
 * agent_task_queue per request, not a filter on the existing query.
 */
export function AgentScorecardsCard({ wsId, days }: { wsId: string; days: number }) {
  const { t } = useT("usage");
  const { data: rows = [] } = useQuery(workspaceScorecardsOptions(wsId, days));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  if (rows.length === 0) return null;
  const name = (id: string) => agents.find((a) => a.id === id)?.name ?? id.slice(0, 8);
  const pct = (v: number | null) => (v === null ? "—" : `${v}%`);
  return (
    <div data-testid="agent-scorecards" className="rounded-lg border bg-card">
      <div className="flex flex-wrap items-baseline justify-between gap-2 px-4 pt-3">
        <span className="text-caption font-medium">
          {t(($) => $.scorecards.title, { days })}
        </span>
        <span className="text-caption text-muted-foreground">
          {t(($) => $.scorecards.scope_note)}
        </span>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-caption">
          <thead className="text-muted-foreground">
            <tr>
              <th className="px-4 py-2 text-left font-normal">{t(($) => $.scorecards.agent)}</th>
              <th className="px-2 py-2 text-right font-normal">{t(($) => $.scorecards.runs)}</th>
              <th className="px-2 py-2 text-right font-normal">{t(($) => $.scorecards.accepted)}</th>
              <th className="px-2 py-2 text-right font-normal">{t(($) => $.scorecards.failed)}</th>
              <th className="px-2 py-2 text-right font-normal">{t(($) => $.scorecards.reopened)}</th>
              <th className="px-2 py-2 text-right font-normal">{t(($) => $.scorecards.no_intervention)}</th>
              <th className="px-4 py-2 text-right font-normal">{t(($) => $.scorecards.mean_cost)}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={`${r.agent_id}-${r.runtime_id ?? ""}`} data-testid="scorecard-row" className={cn("border-t", r.low_sample && "text-muted-foreground")}>
                <td className="px-4 py-1.5">
                  {name(r.agent_id)}
                  {r.low_sample && <span className="ml-1 text-faint-foreground">{t(($) => $.scorecards.low_sample)}</span>}
                </td>
                <td className="px-2 py-1.5 text-right tabular-nums">{r.runs_total}</td>
                <td className="px-2 py-1.5 text-right tabular-nums">{pct(scorecardRate(r.runs_accepted, r.runs_total))}</td>
                <td className="px-2 py-1.5 text-right tabular-nums">{pct(scorecardRate(r.runs_failed, r.runs_total))}</td>
                <td className="px-2 py-1.5 text-right tabular-nums">{pct(scorecardRate(r.runs_reopened, r.runs_total))}</td>
                <td className="px-2 py-1.5 text-right tabular-nums">{pct(scorecardRate(r.runs_no_intervention, r.runs_total))}</td>
                <td className="px-4 py-1.5 text-right tabular-nums">{r.runs_total > 0 ? formatUsd(r.cost_usd_ticks_total / 1e10 / r.runs_total) : "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
