"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { GitFork } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { agentListOptions } from "@multica/core/workspace/queries";
import { fanoutProgress, issueFanoutOptions, useStartFanout } from "@multica/core/issues/fanout";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../../navigation";
import { useT, useTimeAgo } from "../../i18n";

/**
 * Fan-out / fan-in (K38): the sub-tasks launched in parallel from this
 * issue, each with its live run status, the overall barrier, and the
 * synthesis run once it settled — with a warning on partial failure. Also
 * the form to launch one (leader + sub-tasks assigned to agents).
 */
export function FanoutSection({ issueId, canManage = true }: { issueId: string; canManage?: boolean }) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { data: batch } = useQuery(issueFanoutOptions(wsId, issueId));
  const { data: agents = [] } = useQuery({ ...agentListOptions(wsId), enabled: canManage });
  const start = useStartFanout(wsId, issueId);
  const [open, setOpen] = useState(false);
  const [leader, setLeader] = useState("");
  const [rows, setRows] = useState<{ description: string; assignee_id: string }[]>([{ description: "", assignee_id: "" }]);
  const fail = (e: unknown) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.fanout.failed));
  const agentName = (id: string) => agents.find((a) => a.id === id)?.name ?? id.slice(0, 8);
  const pending = batch?.status === "pending";
  const valid = leader !== "" && rows.length > 0 && rows.every((r) => r.description.trim() !== "" && r.assignee_id !== "");
  if (!batch && (!canManage || agents.length === 0)) return null;

  return (
    <div data-testid="fanout-section" className="flex flex-col gap-2 text-caption">
      {batch && (
        <div data-testid="fanout-batch" data-status={batch.status} className={cn("flex flex-col gap-1.5 rounded-md border p-2", batch.status === "partial_failure" ? "border-warning/60" : "border-border")}>
          <div className="flex items-center gap-2 font-medium">
            <GitFork className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
            <span>{t(($) => $.fanout.section)}</span>
            <span className="text-muted-foreground">{t(($) => $.fanout.count, { done: batch.completed_count + batch.failed_count, total: batch.expected_count })}</span>
            <span className={cn("ml-auto rounded px-1", batch.status === "complete" ? "bg-success/15 text-success" : batch.status === "partial_failure" ? "bg-warning/20 text-warning" : "bg-muted text-muted-foreground")}>{t(($) => $.fanout.status[batch.status])}</span>
          </div>
          <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted"><div className={cn("h-full", batch.failed_count > 0 ? "bg-warning" : "bg-success")} style={{ width: `${Math.round(fanoutProgress(batch) * 100)}%` }} /></div>
          <ul className="flex flex-col gap-0.5">
            {batch.members.map((m) => (
              <li key={m.id} data-testid="fanout-member" data-outcome={m.outcome ?? "pending"} className="flex items-center gap-2">
                <span className={cn("h-1.5 w-1.5 shrink-0 rounded-full", m.outcome === "completed" ? "bg-success" : m.outcome === "failed" ? "bg-destructive" : "bg-info animate-pulse")} />
                <AppLink href={paths.issueDetail(m.child_issue_id)} className="min-w-0 flex-1 truncate hover:underline">{m.description}</AppLink>
                <span className="text-muted-foreground">{agentName(m.assignee_agent_id)}</span>
                <span className="font-mono text-muted-foreground">{m.outcome ?? m.task_status}</span>
              </li>
            ))}
          </ul>
          {batch.status === "partial_failure" && (
            <p data-testid="fanout-warning" className="text-warning">{t(($) => $.fanout.partial, { failed: batch.failed_count })}</p>
          )}
          {batch.synthesis_task_id && (
            <p className="text-muted-foreground">
              {t(($) => $.fanout.synthesis, { id: batch.synthesis_task_id.slice(0, 8) })}
              {batch.completed_at && <span> · {timeAgo(batch.completed_at)}</span>}
            </p>
          )}
        </div>
      )}
      {canManage && !pending && agents.length > 0 && (
        open ? (
          <form data-testid="fanout-form" className="flex flex-col gap-1.5 rounded-md border border-border p-2" onSubmit={(e) => { e.preventDefault(); if (valid) start.mutate({ leader_agent_id: leader, sub_tasks: rows.map((r) => ({ description: r.description.trim(), assignee_id: r.assignee_id })) }, { onError: fail, onSuccess: () => { setOpen(false); setRows([{ description: "", assignee_id: "" }]); } }); }}>
            <select aria-label={t(($) => $.fanout.leader)} className="w-64 rounded-md border border-input bg-transparent px-2 py-1" value={leader} onChange={(e) => setLeader(e.target.value)}>
              <option value="">{t(($) => $.fanout.leader)}</option>
              {agents.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
            </select>
            {rows.map((r, i) => (
              <div key={i} className="flex gap-1">
                <Input aria-label={t(($) => $.fanout.sub_task, { n: i + 1 })} placeholder={t(($) => $.fanout.sub_task_placeholder)} value={r.description} onChange={(e) => setRows(rows.map((x, n) => (n === i ? { ...x, description: e.target.value } : x)))} />
                <select aria-label={t(($) => $.fanout.assignee, { n: i + 1 })} className="rounded-md border border-input bg-transparent px-2 py-1" value={r.assignee_id} onChange={(e) => setRows(rows.map((x, n) => (n === i ? { ...x, assignee_id: e.target.value } : x)))}>
                  <option value="">{t(($) => $.fanout.pick_agent)}</option>
                  {agents.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
                </select>
                <button type="button" aria-label={t(($) => $.fanout.remove, { n: i + 1 })} className="text-muted-foreground hover:text-destructive" onClick={() => setRows(rows.filter((_, n) => n !== i))}>×</button>
              </div>
            ))}
            <div className="flex gap-1">
              <Button type="button" size="sm" variant="outline" onClick={() => setRows([...rows, { description: "", assignee_id: "" }])}>{t(($) => $.fanout.add)}</Button>
              <Button type="submit" size="sm" disabled={!valid || start.isPending}>{t(($) => $.fanout.launch)}</Button>
              <Button type="button" size="sm" variant="ghost" onClick={() => setOpen(false)}>{t(($) => $.fanout.cancel)}</Button>
            </div>
          </form>
        ) : (
          <button type="button" className="self-start text-muted-foreground hover:text-foreground" onClick={() => setOpen(true)}>{t(($) => $.fanout.open)}</button>
        )
      )}
    </div>
  );
}
