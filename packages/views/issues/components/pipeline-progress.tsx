"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ListOrdered } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { issuePipelineRunOptions, pipelinesOptions, stageStates, useAdvancePipelineRun, useCancelPipelineRun, useStartPipelineRun } from "@multica/core/pipelines";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

/**
 * Pipelines (K37): the issue's progress through its pipeline, one bar per
 * stage — done, current, waiting on a gate, to do — with the gate's
 * Decision Card living in the decisions section above. Without a run: a
 * picker to start one.
 */
export function PipelineProgress({ issueId, canManage = true }: { issueId: string; canManage?: boolean }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: run } = useQuery(issuePipelineRunOptions(wsId, issueId));
  const { data: pipelines = [] } = useQuery({ ...pipelinesOptions(wsId), enabled: !run || run.status === "completed" || run.status === "cancelled" });
  const start = useStartPipelineRun(wsId, issueId);
  const advance = useAdvancePipelineRun(wsId, issueId);
  const cancel = useCancelPipelineRun(wsId, issueId);
  const [picked, setPicked] = useState("");
  const fail = (e: unknown) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.pipeline.failed));
  const open = run && (run.status === "active" || run.status === "paused");

  if (!open) {
    if (pipelines.length === 0 || !canManage) return null;
    return (
      <div data-testid="pipeline-start" className="flex flex-wrap items-center gap-2 px-2 py-1 text-caption">
        <ListOrdered className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
        <select aria-label={t(($) => $.pipeline.pick)} className="rounded-md border border-input bg-transparent px-2 py-1" value={picked} onChange={(e) => setPicked(e.target.value)}>
          <option value="">{t(($) => $.pipeline.pick)}</option>
          {pipelines.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
        </select>
        <Button type="button" size="sm" variant="outline" disabled={picked === "" || start.isPending} onClick={() => start.mutate(picked, { onError: fail })}>{t(($) => $.pipeline.start)}</Button>
        {run && <span className="text-muted-foreground">{t(($) => $.pipeline.last, { name: run.pipeline_name, status: t(($) => $.pipeline.status[run.status]) })}</span>}
      </div>
    );
  }
  const states = stageStates(run);
  return (
    <div data-testid="pipeline-progress" data-status={run.status} className="flex flex-col gap-1.5 px-2 py-1 text-caption">
      <div className="flex items-center gap-2">
        <ListOrdered className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
        <span className="font-medium">{run.pipeline_name}</span>
        {run.last_error && <span data-testid="pipeline-error" className="rounded bg-destructive/15 px-1 text-destructive">{run.last_error}</span>}
        {run.status === "paused" && run.gate_decision_id && <span className="rounded bg-warning/20 px-1 text-warning">{t(($) => $.pipeline.gate_waiting)}</span>}
        {canManage && (
          <span className="ml-auto flex gap-1">
            {run.status === "paused" && <Button type="button" size="sm" variant="outline" disabled={advance.isPending} onClick={() => advance.mutate(run.id, { onError: fail })}>{run.gate_decision_id ? t(($) => $.pipeline.approve) : t(($) => $.pipeline.retry)}</Button>}
            <Button type="button" size="sm" variant="ghost" disabled={cancel.isPending} onClick={() => cancel.mutate(run.id, { onError: fail })}>{t(($) => $.pipeline.cancel)}</Button>
          </span>
        )}
      </div>
      <ol className="flex items-center gap-1">
        {run.stages.map((s, i) => (
          <li key={s.id} data-testid="pipeline-stage" data-state={states[i]} className="flex min-w-0 flex-1 flex-col gap-0.5">
            <span className={cn("h-1.5 rounded-full", states[i] === "done" ? "bg-success" : states[i] === "current" ? "bg-info animate-pulse" : states[i] === "gate" ? "bg-warning" : "bg-muted")} />
            <span className={cn("truncate", states[i] === "todo" ? "text-muted-foreground" : "")}>
              {s.requires_human_gate && "⏸ "}{s.name}
            </span>
          </li>
        ))}
      </ol>
    </div>
  );
}
