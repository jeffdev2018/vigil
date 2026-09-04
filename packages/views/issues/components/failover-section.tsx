"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, Shuffle } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { runtimeDisplayLabel, runtimeListOptions } from "@multica/core/runtimes";
import { issueFailoverHistoryOptions } from "@multica/core/runtimes/pools";
import { useT, useTimeAgo } from "../../i18n";

/**
 * Runtime failover (K28): which runs of this issue moved runtime, from
 * where to where and why; a run on the degraded runtime gets a banner, not
 * a hint. Renders nothing until a run moved.
 */
export function FailoverSection({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const { data: runs = [] } = useQuery(issueFailoverHistoryOptions(wsId, issueId));
  const { data: runtimes = [] } = useQuery({ ...runtimeListOptions(wsId), enabled: runs.length > 0 });
  if (runs.length === 0) return null;
  const name = (id: string) => {
    const r = runtimes.find((x) => x.id === id);
    return r ? runtimeDisplayLabel(r) : id.slice(0, 8);
  };
  const degradedNow = runs.some((r) => r.degraded && (r.status === "queued" || r.status === "dispatched" || r.status === "running" || r.status === "deferred"));

  return (
    <div data-testid="failover-section" className="text-caption">
      {degradedNow && (
        <div role="alert" data-testid="degraded-banner" className="mb-2 flex items-center gap-2 rounded-md border border-warning bg-warning/10 px-2 py-1.5 font-medium text-warning">
          <AlertTriangle className="h-4 w-4 shrink-0" aria-hidden="true" />
          {t(($) => $.failover.degraded_banner)}
        </div>
      )}
      <div className="mb-2 flex items-center gap-1 px-2 py-1 font-medium">
        <Shuffle className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
        <span>{t(($) => $.failover.section)}</span>
      </div>
      <div className="flex flex-col gap-2 pl-2">
        {runs.map((run) => (
          <div key={run.task_id} data-testid="failover-run" data-degraded={run.degraded ? "true" : "false"} className="rounded-md border border-border p-2">
            <div className="mb-1 flex items-center gap-2 font-mono text-micro text-muted-foreground">
              <span>{t(($) => $.failover.run, { id: run.task_id.slice(0, 8) })}</span>
              <span>· {run.status}</span>
              {run.degraded && <span className="rounded bg-warning/20 px-1 font-sans text-warning">{t(($) => $.failover.degraded_badge)}</span>}
              {run.failure_reason === "runtime_pool_exhausted" && <span className="rounded bg-destructive/15 px-1 font-sans text-destructive">{t(($) => $.failover.exhausted)}</span>}
            </div>
            <ul className="flex flex-col gap-0.5">
              {run.moves.map((m, i) => (
                <li key={i} className="flex flex-wrap items-center gap-1">
                  <span>{name(m.from_runtime_id)}</span>
                  <span className="text-muted-foreground">→</span>
                  <span className={m.degraded ? "text-warning" : ""}>{name(m.to_runtime_id)}</span>
                  <span className="text-muted-foreground">· {t(($) => $.failover.reasons[m.reason as "runtime_offline"] ?? m.reason)}</span>
                  {m.at && <span className="ml-auto text-muted-foreground">{timeAgo(m.at)}</span>}
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>
    </div>
  );
}
