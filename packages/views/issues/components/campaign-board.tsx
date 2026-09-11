"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { GitMerge } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { agentListOptions } from "@multica/core/workspace/queries";
import { campaignProgress, campaignShardSkippable, issueCampaignOptions, useCreateCampaign, useSkipCampaignShard, type CampaignMergeStatus } from "@multica/core/issues/campaign";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";

const mergeTone: Record<CampaignMergeStatus, string> = {
  pending: "bg-muted text-muted-foreground",
  ready: "bg-info/15 text-info",
  rebasing: "bg-info/15 text-info animate-pulse",
  merged: "bg-success/15 text-success",
  conflict: "bg-destructive/15 text-destructive",
  skipped: "bg-muted text-muted-foreground line-through",
};

/**
 * Refactoring campaign board (K42): one row per shard with its run status,
 * queue position and merge status; the overall merge progress; a skip
 * button on shards a human may take out of the queue (a conflict holds its
 * position but never hides the rows behind it). Also the launch form.
 */
export function CampaignBoard({ issueId, canManage = true }: { issueId: string; canManage?: boolean }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { data: campaign } = useQuery(issueCampaignOptions(wsId, issueId));
  const { data: agents = [] } = useQuery({ ...agentListOptions(wsId), enabled: canManage });
  const create = useCreateCampaign(wsId, issueId);
  const skip = useSkipCampaignShard(wsId, issueId);
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [target, setTarget] = useState("main");
  const [leader, setLeader] = useState("");
  const emptyRow = { description: "", assignee_id: "", branch_name: "" };
  const [rows, setRows] = useState([emptyRow]);
  const fail = (e: unknown) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.campaign.failed));
  const agentName = (id: string) => agents.find((a) => a.id === id)?.name ?? t(($) => $.campaign.unknown_agent);
  const active = campaign?.status === "running" || campaign?.status === "merging";
  const valid = name.trim() !== "" && target.trim() !== "" && leader !== "" && rows.length > 0 && rows.every((r) => r.description.trim() !== "" && r.assignee_id !== "");
  if (!campaign && (!canManage || agents.length === 0)) return null;
  const done = campaign ? campaign.shards.filter((s) => s.merge_status === "merged" || s.merge_status === "skipped").length : 0;

  return (
    <div data-testid="campaign-section" className="flex flex-col gap-2 text-caption">
      {campaign && (
        <div data-testid="campaign" data-status={campaign.status} className="flex flex-col gap-1.5 rounded-md border border-border p-2">
          <div className="flex items-center gap-2 font-medium">
            <GitMerge className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
            <span>{t(($) => $.campaign.section)}</span>
            <span className="truncate">{campaign.name}</span>
            <span className="font-mono text-muted-foreground">{t(($) => $.campaign.target, { branch: campaign.target_branch })}</span>
            <span className={cn("ml-auto rounded px-1", campaign.status === "completed" ? "bg-success/15 text-success" : campaign.status === "failed" ? "bg-destructive/15 text-destructive" : "bg-muted text-muted-foreground")}>{t(($) => $.campaign.status[campaign.status])}</span>
          </div>
          <div className="flex items-center gap-2">
            <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted"><div className="h-full bg-success" style={{ width: `${Math.round(campaignProgress(campaign) * 100)}%` }} /></div>
            <span className="text-muted-foreground">{t(($) => $.campaign.progress, { done, total: campaign.shards.length })}</span>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full">
              <tbody>
                {campaign.shards.map((s) => (
                  <tr key={s.id} data-testid="campaign-shard" data-merge-status={s.merge_status} className="align-top">
                    <td className="pr-2 font-mono text-muted-foreground">{t(($) => $.campaign.position, { n: s.merge_position + 1 })}</td>
                    <td className="min-w-0 pr-2">
                      <AppLink href={paths.issueDetail(s.child_issue_id)} className="hover:underline">{s.description}</AppLink>
                      <div className="font-mono text-muted-foreground">{s.branch_name} · {agentName(s.assignee_agent_id)}</div>
                      {s.merge_status === "conflict" && <div data-testid="campaign-conflict" className="text-destructive">{s.blockers.map((b) => b.label).join("; ")} {t(($) => $.campaign.conflict_hint)}</div>}
                      {s.merge_status === "pending" && s.blockers.length > 0 && <div className="text-muted-foreground">{s.blockers.map((b) => b.label).join("; ")}</div>}
                    </td>
                    <td className="pr-2 font-mono text-muted-foreground">{s.run_outcome ?? s.task_status}</td>
                    <td className="pr-2"><span className={cn("rounded px-1", mergeTone[s.merge_status])}>{t(($) => $.campaign.merge_status[s.merge_status])}</span></td>
                    <td>
                      {canManage && active && campaignShardSkippable(s) && (
                        <button type="button" className="text-muted-foreground hover:text-destructive" disabled={skip.isPending} onClick={() => skip.mutate(s.id, { onError: fail })}>{t(($) => $.campaign.skip, { n: s.merge_position + 1 })}</button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
      {canManage && !active && agents.length > 0 && (
        open ? (
          <form data-testid="campaign-form" className="flex flex-col gap-1.5 rounded-md border border-border p-2" onSubmit={(e) => { e.preventDefault(); if (valid) create.mutate({ name: name.trim(), target_branch: target.trim(), leader_agent_id: leader, shards: rows.map((r) => ({ description: r.description.trim(), assignee_id: r.assignee_id, ...(r.branch_name.trim() ? { branch_name: r.branch_name.trim() } : {}) })) }, { onError: fail, onSuccess: () => { setOpen(false); setRows([emptyRow]); setName(""); } }); }}>
            <div className="flex gap-1">
              <Input aria-label={t(($) => $.campaign.name)} placeholder={t(($) => $.campaign.name)} value={name} onChange={(e) => setName(e.target.value)} />
              <Input aria-label={t(($) => $.campaign.target_branch)} placeholder={t(($) => $.campaign.target_branch)} value={target} onChange={(e) => setTarget(e.target.value)} />
              <Select
                items={[
                  { value: "", label: t(($) => $.campaign.leader) },
                  ...agents.map((a) => ({ value: a.id, label: a.name })),
                ]}
                value={leader}
                onValueChange={(value) => value !== null && setLeader(value)}
              >
                <SelectTrigger size="sm" aria-label={t(($) => $.campaign.leader)}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="">{t(($) => $.campaign.leader)}</SelectItem>
                  {agents.map((a) => <SelectItem key={a.id} value={a.id}>{a.name}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
            {rows.map((r, i) => (
              <div key={i} className="flex gap-1">
                <Input aria-label={t(($) => $.campaign.shard, { n: i + 1 })} placeholder={t(($) => $.campaign.shard_placeholder)} value={r.description} onChange={(e) => setRows(rows.map((x, n) => (n === i ? { ...x, description: e.target.value } : x)))} />
                <Input aria-label={t(($) => $.campaign.branch, { n: i + 1 })} placeholder={t(($) => $.campaign.branch_placeholder)} className="w-48" value={r.branch_name} onChange={(e) => setRows(rows.map((x, n) => (n === i ? { ...x, branch_name: e.target.value } : x)))} />
                <Select
                  items={[
                    { value: "", label: t(($) => $.campaign.pick_agent) },
                    ...agents.map((a) => ({ value: a.id, label: a.name })),
                  ]}
                  value={r.assignee_id}
                  onValueChange={(value) => value !== null && setRows(rows.map((x, n) => (n === i ? { ...x, assignee_id: value } : x)))}
                >
                  <SelectTrigger size="sm" aria-label={t(($) => $.campaign.assignee, { n: i + 1 })}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="">{t(($) => $.campaign.pick_agent)}</SelectItem>
                    {agents.map((a) => <SelectItem key={a.id} value={a.id}>{a.name}</SelectItem>)}
                  </SelectContent>
                </Select>
                <button type="button" aria-label={t(($) => $.campaign.remove, { n: i + 1 })} className="text-muted-foreground hover:text-destructive" onClick={() => setRows(rows.filter((_, n) => n !== i))}>×</button>
              </div>
            ))}
            <div className="flex gap-1">
              <Button type="button" size="sm" variant="outline" onClick={() => setRows([...rows, emptyRow])}>{t(($) => $.campaign.add)}</Button>
              <Button type="submit" size="sm" disabled={!valid || create.isPending}>{t(($) => $.campaign.launch)}</Button>
              <Button type="button" size="sm" variant="ghost" onClick={() => setOpen(false)}>{t(($) => $.campaign.cancel)}</Button>
            </div>
          </form>
        ) : (
          <button type="button" className="self-start text-muted-foreground hover:text-foreground" onClick={() => setOpen(true)}>{t(($) => $.campaign.open)}</button>
        )
      )}
    </div>
  );
}
