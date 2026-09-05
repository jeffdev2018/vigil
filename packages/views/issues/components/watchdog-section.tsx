"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Eye } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  issueWatchdogOptions,
  issueWatchdogVerdictsOptions,
  useDeleteIssueWatchdog,
  useReviewWatchdogVerdict,
  useScanIssueWatchdogNow,
  useSetIssueWatchdog,
  watchdogOutcome,
  type WatchdogVerdict,
} from "@multica/core/issues/watchdog";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { useT, useTimeAgo } from "../../i18n";

/**
 * Task watchdog (K73): an optional agent, different from the assignee, that
 * inspects this issue and its subtree once they are at rest and returns a
 * verdict. The section configures it (agent, human owner, rest, instructions)
 * and lists its verdicts with what each one did; the owner confirms or
 * overturns a verdict, which feeds the watchdog's trust tier.
 */
export function WatchdogSection({ issueId, canManage = true }: { issueId: string; canManage?: boolean }) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const { data: watchdog, isPending } = useQuery(issueWatchdogOptions(wsId, issueId));
  const { data: verdicts = [] } = useQuery({ ...issueWatchdogVerdictsOptions(wsId, issueId), enabled: !!watchdog });
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const save = useSetIssueWatchdog(wsId, issueId);
  const remove = useDeleteIssueWatchdog(wsId, issueId);
  const scan = useScanIssueWatchdogNow(wsId, issueId);
  const review = useReviewWatchdogVerdict(wsId, issueId);
  const [editing, setEditing] = useState(false);
  const [agentId, setAgentId] = useState("");
  const [ownerId, setOwnerId] = useState("");
  const [instructions, setInstructions] = useState("");
  const [rest, setRest] = useState(30);
  useEffect(() => {
    if (watchdog) {
      setAgentId(watchdog.agent_id);
      setOwnerId(watchdog.owner_id);
      setInstructions(watchdog.instructions);
      setRest(watchdog.rest_minutes);
    }
  }, [watchdog]);
  if (isPending) return null;
  if (!watchdog && !canManage) return null;

  const fail = (e: unknown, fallback: string) => toast.error(e instanceof Error && e.message ? e.message : fallback);
  const submit = () =>
    save.mutate(
      { agent_id: agentId, owner_id: ownerId || undefined, instructions, rest_minutes: rest, enabled: watchdog?.enabled ?? true },
      { onSuccess: () => { setEditing(false); toast.success(t(($) => $.watchdog.saved)); }, onError: (e) => fail(e, t(($) => $.watchdog.save_failed)) },
    );
  const form = (
    <div data-testid="watchdog-form" className="flex flex-col gap-2">
      <label className="flex flex-col gap-0.5">
        <span className="text-muted-foreground">{t(($) => $.watchdog.agent)}</span>
        <select aria-label={t(($) => $.watchdog.agent)} className="rounded border bg-background p-1" value={agentId} onChange={(e) => setAgentId(e.target.value)}>
          <option value="">{t(($) => $.watchdog.pick_agent)}</option>
          {agents.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
        </select>
      </label>
      <label className="flex flex-col gap-0.5">
        <span className="text-muted-foreground">{t(($) => $.watchdog.owner)}</span>
        <select aria-label={t(($) => $.watchdog.owner)} className="rounded border bg-background p-1" value={ownerId} onChange={(e) => setOwnerId(e.target.value)}>
          <option value="">{t(($) => $.watchdog.owner_me)}</option>
          {members.map((m) => <option key={m.user_id} value={m.user_id}>{m.name || m.email || m.user_id}</option>)}
        </select>
      </label>
      <label className="flex items-center gap-2">
        <span className="text-muted-foreground">{t(($) => $.watchdog.rest)}</span>
        <input type="number" min={1} max={1440} aria-label={t(($) => $.watchdog.rest)} className="w-20 rounded border bg-background p-1" value={rest} onChange={(e) => setRest(Number(e.target.value))} />
      </label>
      <Textarea rows={2} aria-label={t(($) => $.watchdog.instructions)} placeholder={t(($) => $.watchdog.instructions_placeholder)} value={instructions} onChange={(e) => setInstructions(e.target.value)} />
      <div className="flex gap-2">
        <Button type="button" size="sm" disabled={!agentId || save.isPending} onClick={submit}>{t(($) => $.watchdog.save)}</Button>
        {watchdog && <Button type="button" size="sm" variant="ghost" onClick={() => setEditing(false)}>{t(($) => $.watchdog.cancel)}</Button>}
      </div>
    </div>
  );

  return (
    <div data-testid="watchdog" data-state={watchdog ? (watchdog.enabled ? "on" : "off") : "none"} className="flex flex-col gap-2 rounded-md border p-2 text-caption">
      <div className="flex flex-wrap items-center gap-2 font-medium">
        <Eye className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
        <span>{t(($) => $.watchdog.section)}</span>
        {watchdog && <span className="font-normal text-muted-foreground">{t(($) => $.watchdog.by, { name: watchdog.agent_name || watchdog.agent_id.slice(0, 8), rest: watchdog.rest_minutes })}</span>}
        {watchdog && canManage && (
          <div className="ml-auto flex items-center gap-2">
            <Switch
              aria-label={t(($) => $.watchdog.enabled)}
              checked={watchdog.enabled}
              disabled={save.isPending}
              onCheckedChange={(on) => save.mutate({ agent_id: watchdog.agent_id, owner_id: watchdog.owner_id, instructions: watchdog.instructions, rest_minutes: watchdog.rest_minutes, enabled: on }, { onError: (e) => fail(e, t(($) => $.watchdog.save_failed)) })}
            />
            <Button type="button" size="sm" variant="outline" disabled={scan.isPending} onClick={() => scan.mutate(undefined, { onSuccess: () => toast.success(t(($) => $.watchdog.scan_started)), onError: (e) => fail(e, t(($) => $.watchdog.scan_failed)) })}>
              {t(($) => $.watchdog.scan_now)}
            </Button>
            <Button type="button" size="sm" variant="ghost" onClick={() => setEditing((v) => !v)}>{t(($) => $.watchdog.edit)}</Button>
            <Button type="button" size="sm" variant="ghost" className="text-destructive" disabled={remove.isPending} onClick={() => remove.mutate(undefined, { onSuccess: () => toast.success(t(($) => $.watchdog.removed)), onError: (e) => fail(e, t(($) => $.watchdog.save_failed)) })}>
              {t(($) => $.watchdog.remove)}
            </Button>
          </div>
        )}
      </div>
      {!watchdog && <p className="text-muted-foreground">{t(($) => $.watchdog.intro)}</p>}
      {(!watchdog || editing) && canManage && form}
      {watchdog && watchdog.last_scanned_at && (
        <p className="text-muted-foreground">{t(($) => $.watchdog.last_scan, { when: timeAgo(watchdog.last_scanned_at) })}{watchdog.motion_streak > 0 && <> · {t(($) => $.watchdog.streak, { n: watchdog.motion_streak })}</>}</p>
      )}
      {watchdog && verdicts.length > 0 && (
        <ul className="flex flex-col gap-1.5">
          {verdicts.map((v) => <VerdictRow key={v.id} verdict={v} canManage={canManage} onReview={(confirmed) => review.mutate({ verdictId: v.id, confirmed }, { onError: (e) => fail(e, t(($) => $.watchdog.review_failed)) })} />)}
        </ul>
      )}
    </div>
  );
}

function VerdictRow({ verdict: v, canManage, onReview }: { verdict: WatchdogVerdict; canManage: boolean; onReview: (confirmed: boolean) => void }) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const outcome = watchdogOutcome(v);
  return (
    <li data-testid="watchdog-verdict" data-verdict={v.verdict} data-outcome={outcome} className="flex flex-col gap-1 rounded border p-2">
      <div className="flex flex-wrap items-center gap-2">
        <span className={cn("rounded px-1.5 font-medium", v.verdict === "legitimate" && "bg-success/15 text-success", v.verdict === "motion" && "bg-warning/15 text-warning", v.verdict === "escalate" && "bg-destructive/10 text-destructive")}>
          {t(($) => $.watchdog.verdicts[v.verdict])}
        </span>
        <span className="text-muted-foreground">{t(($) => $.watchdog.outcomes[outcome])}</span>
        <span className="text-muted-foreground">{timeAgo(v.created_at)}</span>
        {v.human_review !== "pending" && <span className="rounded bg-muted px-1 text-muted-foreground">{t(($) => $.watchdog.reviews[v.human_review as "confirmed" | "overturned"])}</span>}
        {canManage && v.human_review === "pending" && !v.decision_id && (
          <span className="ml-auto flex gap-1">
            <Button type="button" size="sm" variant="ghost" onClick={() => onReview(true)}>{t(($) => $.watchdog.confirm)}</Button>
            <Button type="button" size="sm" variant="ghost" onClick={() => onReview(false)}>{t(($) => $.watchdog.overturn)}</Button>
          </span>
        )}
      </div>
      {v.summary && <p className="text-muted-foreground">{v.summary}</p>}
      {v.findings.length > 0 && (
        <ul className="list-disc pl-4">
          {v.findings.map((f, i) => (
            <li key={i}>
              <span className="font-medium">{f.issue}</span> · {t(($) => $.watchdog.actions[f.action as "reopen" | "ask_proof" | "none"] ?? $.watchdog.actions.none)}
              {f.reason && <> · {f.reason}</>}
              {f.missing_criterion && <> · {t(($) => $.watchdog.missing, { criterion: f.missing_criterion })}</>}
            </li>
          ))}
        </ul>
      )}
      {v.dropped.length > 0 && <p className="text-muted-foreground">{t(($) => $.watchdog.dropped, { n: v.dropped.length })}</p>}
    </li>
  );
}
