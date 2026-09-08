"use client";

import { useQuery } from "@tanstack/react-query";
import { TrendingDown, TrendingUp } from "lucide-react";
import { dashboardAgentRoiOptions, roiTrendPct } from "@multica/core/dashboard/queries";
import type { AgentRoiRow } from "@multica/core/types";
import { CurrencyNumberFlow } from "@multica/ui/components/ui/number-flow";
import { cn } from "@multica/ui/lib/utils";
import { formatUsd } from "../../runtimes/utils";
import { RESTRICTED_AGENTS_ROW_ID } from "../utils";
import { useT } from "../../i18n";

/**
 * ROI per agent (JEF-252): one row per agent, what it cost against what it
 * closed, so "keep paying for this one" is a glance rather than a spreadsheet.
 *
 * An agent that closed nothing keeps its row with a "—" ratio: it is the row a
 * purchase decision most needs to see, and showing $0.00 there would rank it as
 * the cheapest agent in the workspace. The server sorts cheapest-first with
 * those rows last; this component renders that order as it comes.
 */
export function AgentRoiCard({
  wsId,
  days,
  projectId,
  tz,
  locales,
}: {
  wsId: string;
  days: number;
  projectId: string | null;
  tz: string;
  locales: string;
}) {
  const { t } = useT("usage");
  const { data, isError } = useQuery(dashboardAgentRoiOptions(wsId, days, projectId, tz));
  // Defensive on shape: an older backend (or a test fixture) may hand back
  // something that is not this response.
  if (!data || isError || !Array.isArray(data.agents)) return null;
  const agents = data.agents;
  if (agents.length === 0) {
    return (
      <div data-testid="agent-roi" data-empty="true" className="rounded-lg border bg-card px-4 py-3 text-caption text-muted-foreground">
        {t(($) => $.agent_roi.empty, { days })}
      </div>
    );
  }
  const name = (row: AgentRoiRow) =>
    row.agent_id === RESTRICTED_AGENTS_ROW_ID
      ? t(($) => $.leaderboard.other_agents)
      : row.agent_name || row.agent_id.slice(0, 8);
  // Only agents with a ratio can be compared; the server already ordered them
  // cheapest first, so the headline is the first two of those.
  const ranked = agents.filter((a) => a.cost_per_issue_usd_ticks !== null);
  const [best, second] = ranked;
  const usd = (ticks: number) => formatUsd(ticks / 1e10);

  return (
    <div data-testid="agent-roi" className="rounded-lg border bg-card">
      <div className="flex flex-wrap items-baseline justify-between gap-2 px-4 pt-3">
        <span className="text-caption font-medium">{t(($) => $.agent_roi.title, { days })}</span>
      </div>
      {best?.cost_per_issue_usd_ticks != null && (
        <p data-testid="agent-roi-headline" className="px-4 pt-1 text-caption text-muted-foreground">
          {second?.cost_per_issue_usd_ticks != null
            ? t(($) => $.agent_roi.headline, {
                best: name(best),
                count: best.issues_closed,
                cost: usd(best.cost_per_issue_usd_ticks),
                cost2: usd(second.cost_per_issue_usd_ticks),
                second: name(second),
              })
            : t(($) => $.agent_roi.headline_single, {
                best: name(best),
                count: best.issues_closed,
                cost: usd(best.cost_per_issue_usd_ticks),
              })}
        </p>
      )}
      <div className="overflow-x-auto">
        <table className="mt-2 w-full text-caption">
          <thead className="text-muted-foreground">
            <tr>
              <th className="px-4 py-2 text-left font-normal">{t(($) => $.agent_roi.agent)}</th>
              <th className="px-2 py-2 text-right font-normal">{t(($) => $.agent_roi.issues)}</th>
              <th className="px-2 py-2 text-right font-normal">{t(($) => $.agent_roi.prs)}</th>
              <th className="px-2 py-2 text-right font-normal">{t(($) => $.agent_roi.cost)}</th>
              <th className="px-4 py-2 text-right font-normal">{t(($) => $.agent_roi.cost_per_issue)}</th>
            </tr>
          </thead>
          <tbody>
            {agents.map((row) => {
              const trend = roiTrendPct(row.cost_per_issue_usd_ticks, row.prev_cost_per_issue_usd_ticks);
              return (
                <tr key={row.agent_id} data-testid="agent-roi-row" className="border-t">
                  <td className="px-4 py-1.5">
                    <span className={cn(row.agent_id === RESTRICTED_AGENTS_ROW_ID && "italic text-muted-foreground")}>{name(row)}</span>
                    {row.provider && <span className="ml-1 text-muted-foreground">{row.provider}</span>}
                  </td>
                  <td className="px-2 py-1.5 text-right tabular-nums">{row.issues_closed}</td>
                  <td className="px-2 py-1.5 text-right tabular-nums">{row.prs_merged}</td>
                  <td className="px-2 py-1.5 text-right tabular-nums">
                    {usd(row.cost_usd_ticks)}
                    {row.uncosted_runs > 0 && <span className="ml-1 text-muted-foreground">{t(($) => $.agent_roi.floor)}</span>}
                  </td>
                  <td className="px-4 py-1.5 text-right tabular-nums">
                    {row.cost_per_issue_usd_ticks === null ? (
                      <span className="text-muted-foreground" title={t(($) => $.agent_roi.none)}>
                        — <span className="sr-only">{t(($) => $.agent_roi.none)}</span>
                      </span>
                    ) : (
                      <span className="inline-flex items-center justify-end gap-1.5">
                        <span className="font-medium">
                          <CurrencyNumberFlow value={row.cost_per_issue_usd_ticks / 1e10} locales={locales} />
                        </span>
                        {trend !== null && (
                          <span
                            data-testid="agent-roi-trend"
                            className={cn("inline-flex items-center gap-0.5", trend > 0 ? "text-warning" : "text-success")}
                          >
                            {trend > 0 ? <TrendingUp className="size-3" /> : <TrendingDown className="size-3" />}
                            {trend > 0 ? "+" : ""}
                            {trend.toFixed(0)}%
                          </span>
                        )}
                      </span>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
