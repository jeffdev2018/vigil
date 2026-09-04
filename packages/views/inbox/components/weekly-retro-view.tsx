"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { weeklyRetroOptions } from "@multica/core/inbox/queries";
import { usdFromTicks } from "@multica/core/issues/cockpit";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { AppLink } from "../../navigation";
import { useT, useTimeAgo } from "../../i18n";

/**
 * Weekly retro (K34): the last generated retro — runs by outcome, the failed
 * ones with their reason, each agent's week from its scorecard, and the
 * skill proposals section (empty until Skill Miner lands). Without a retro
 * yet, one button generates it now; regenerating is limited server-side to
 * once an hour and the error says so.
 */
export function WeeklyRetroView() {
  const { t } = useT("inbox");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const timeAgo = useTimeAgo();
  const qc = useQueryClient();
  const { data, isLoading, isError } = useQuery(weeklyRetroOptions(wsId));
  const [generating, setGenerating] = useState(false);

  async function generate() {
    setGenerating(true);
    try {
      const retro = await api.regenerateWeeklyRetro();
      qc.setQueryData(weeklyRetroOptions(wsId).queryKey, retro);
      toast.success(t(($) => $.retro.generated));
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t(($) => $.retro.generate_failed));
    } finally {
      setGenerating(false);
    }
  }

  if (isLoading) return <div className="p-4 text-caption text-muted-foreground">{t(($) => $.retro.loading)}</div>;
  if (isError) {
    return (
      <div className="flex flex-col items-start gap-2 p-4 text-caption">
        <span className="text-destructive">{t(($) => $.retro.load_failed)}</span>
        <Button type="button" size="sm" variant="outline" onClick={() => void qc.invalidateQueries({ queryKey: weeklyRetroOptions(wsId).queryKey })}>
          {t(($) => $.retro.retry)}
        </Button>
      </div>
    );
  }
  if (!data) {
    return (
      <div data-testid="retro-empty" className="flex flex-col items-start gap-2 p-4 text-caption text-muted-foreground">
        <span>{t(($) => $.retro.empty)}</span>
        <Button type="button" size="sm" disabled={generating} onClick={() => void generate()}>
          {t(($) => $.retro.generate_now)}
        </Button>
      </div>
    );
  }
  const statuses = Object.entries(data.runs_by_status).sort((a, b) => b[1] - a[1]);
  return (
    <div data-testid="weekly-retro" className="flex flex-col gap-4 overflow-y-auto p-4 text-body">
      <div className="flex items-center gap-2 text-caption text-muted-foreground">
        <span>{t(($) => $.retro.week, { start: data.week_start, end: data.week_end })}</span>
        {data.generated_at && <span>· {t(($) => $.retro.generated_ago, { ago: timeAgo(data.generated_at) })}</span>}
        <span className="flex-1" />
        <Button type="button" size="sm" variant="ghost" disabled={generating} onClick={() => void generate()}>
          {t(($) => $.retro.regenerate)}
        </Button>
      </div>
      {data.narrative && <p className="whitespace-pre-wrap">{data.narrative}</p>}
      <section>
        <h3 className="mb-1 text-caption font-medium">{t(($) => $.retro.runs, { count: data.runs_total })}</h3>
        {data.runs_total === 0 ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.retro.no_runs)}</p>
        ) : (
          <div className="flex flex-wrap gap-1.5 text-caption">
            {statuses.map(([status, n]) => (
              <span key={status} data-testid="retro-status" className="rounded bg-accent px-1.5 py-0.5">
                {status} · {n}
              </span>
            ))}
            {data.median_minutes > 0 && <span className="text-muted-foreground">{t(($) => $.retro.median, { minutes: Math.round(data.median_minutes) })}</span>}
          </div>
        )}
      </section>
      {data.failed.length > 0 && (
        <section>
          <h3 className="mb-1 text-caption font-medium">{t(($) => $.retro.failed, { count: data.failed.length })}</h3>
          <ul className="flex flex-col gap-1 text-caption">
            {data.failed.map((r) => (
              <li key={r.run_id} data-testid="retro-failed" className="flex min-w-0 items-center gap-2">
                <AppLink href={paths.issueDetail(r.issue_id)} className="shrink-0 text-muted-foreground hover:text-foreground">{r.identifier || r.issue_id.slice(0, 8)}</AppLink>
                <span className="truncate">{r.title}</span>
                {r.error && <span className="truncate text-muted-foreground" title={r.error}>· {r.error}</span>}
              </li>
            ))}
          </ul>
        </section>
      )}
      {data.agents.length > 0 && (
        <section>
          <h3 className="mb-1 text-caption font-medium">{t(($) => $.retro.agents)}</h3>
          <div className="overflow-x-auto">
            <table className="w-full text-caption">
              <thead className="text-left text-muted-foreground">
                <tr>
                  <th className="py-1 pr-2 font-normal">{t(($) => $.retro.col_agent)}</th>
                  <th className="py-1 pr-2 font-normal">{t(($) => $.retro.col_runs)}</th>
                  <th className="py-1 pr-2 font-normal">{t(($) => $.retro.col_failed)}</th>
                  <th className="py-1 pr-2 font-normal">{t(($) => $.retro.col_accepted)}</th>
                  <th className="py-1 pr-2 font-normal">{t(($) => $.retro.col_cost)}</th>
                </tr>
              </thead>
              <tbody>
                {data.agents.map((a) => (
                  <tr key={a.agent_id} data-testid="retro-agent">
                    <td className="py-1 pr-2">{a.name || a.agent_id.slice(0, 8)}</td>
                    <td className="py-1 pr-2 tabular-nums">{a.runs_total}</td>
                    <td className="py-1 pr-2 tabular-nums">{a.runs_failed}</td>
                    <td className="py-1 pr-2 tabular-nums">{a.runs_accepted}</td>
                    <td className="py-1 pr-2 tabular-nums">{usdFromTicks(a.cost_usd_ticks)?.toFixed(2) ?? "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}
      <section>
        <h3 className="mb-1 text-caption font-medium">{t(($) => $.retro.skills)}</h3>
        {data.skill_proposals.length === 0 ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.retro.skills_empty)}</p>
        ) : (
          <ul className="flex flex-col gap-1 text-caption">
            {data.skill_proposals.map((p, i) => (
              <li key={i}>{p.text}</li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
