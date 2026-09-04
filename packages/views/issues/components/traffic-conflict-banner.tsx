"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { GitMerge } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { usePauseRun } from "@multica/core/issues/run-control";
import { issueTrafficConflictsOptions, useIgnoreTrafficConflict, type TrafficConflict } from "@multica/core/issues/traffic";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT, useTimeAgo } from "../../i18n";

/**
 * Traffic control (K18): the run is editing paths a human (or another run)
 * is editing. Active conflicts get a banner with the paths and two
 * actions — pause the run (K19) or ignore; settled ones stay as history.
 * Renders nothing without a conflict.
 */
export function TrafficConflictBanner({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const { data: conflicts = [] } = useQuery(issueTrafficConflictsOptions(wsId, issueId));
  const ignore = useIgnoreTrafficConflict(wsId, issueId);
  const pause = usePauseRun(wsId, issueId);
  const [showHistory, setShowHistory] = useState(false);
  const fail = (e: unknown) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.traffic.failed));
  if (conflicts.length === 0) return null;
  const active = conflicts.filter((c) => c.status === "active");
  const settled = conflicts.filter((c) => c.status !== "active");
  const describe = (c: TrafficConflict) => (c.kind === "human" ? t(($) => $.traffic.human) : t(($) => $.traffic.agent, { id: c.other_task_id?.slice(0, 8) ?? "" }));

  return (
    <div data-testid="traffic-conflicts" className="flex flex-col gap-2 text-caption">
      {active.map((c) => (
        <div key={c.id} data-testid="traffic-conflict" data-kind={c.kind} className="flex flex-col gap-1.5 rounded-md border border-warning/60 bg-warning/10 p-2">
          <div className="flex items-center gap-2 font-medium">
            <GitMerge className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
            <span>{describe(c)}</span>
            <span className="ml-auto font-normal text-muted-foreground">{timeAgo(c.created_at)}</span>
          </div>
          <ul className="flex flex-wrap gap-1 font-mono">{c.paths.map((p) => <li key={p} className="rounded bg-background/60 px-1">{p}</li>)}</ul>
          <div className="flex gap-1">
            <Button type="button" size="sm" variant="outline" disabled={pause.isPending} onClick={() => pause.mutate(undefined, { onError: fail })}>{t(($) => $.traffic.pause)}</Button>
            <Button type="button" size="sm" variant="ghost" disabled={ignore.isPending} onClick={() => ignore.mutate(c.id, { onError: fail })}>{t(($) => $.traffic.ignore)}</Button>
          </div>
        </div>
      ))}
      {settled.length > 0 && (
        <button type="button" className="self-start text-muted-foreground hover:text-foreground" onClick={() => setShowHistory((v) => !v)}>
          {showHistory ? t(($) => $.traffic.hide_history) : t(($) => $.traffic.show_history, { count: settled.length })}
        </button>
      )}
      {showHistory && settled.map((c) => (
        <div key={c.id} data-testid="traffic-conflict-settled" className={cn("flex flex-wrap items-center gap-2 rounded-md border border-border/60 px-2 py-1 text-muted-foreground")}>
          <span>{describe(c)}</span>
          <span className="font-mono">{c.paths.join(", ")}</span>
          <span className="ml-auto">{t(($) => $.traffic.status[c.status])}</span>
        </div>
      ))}
    </div>
  );
}
