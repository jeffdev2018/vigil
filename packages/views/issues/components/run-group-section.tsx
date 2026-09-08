"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Flag } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions } from "@multica/core/workspace/queries";
import {
  canStartRunGroup,
  diffStatLabel,
  diffUnifiedLines,
  runGroupErrorKind,
  runGroupKeys,
  runGroupsOptions,
  sortRunGroups,
  useAbandonRunGroup,
  useSettleRunGroup,
  type RunGroup,
  type RunGroupAttempt,
} from "@multica/core/issues/run-group";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { TaskStatusIcon } from "./task-status-icon";
import { useStatusLabel } from "./task-run-labels";
import { RunGroupStartDialog } from "./run-group-start-dialog";

/**
 * Racing attempts (F11): every race this issue has run, newest first, with the
 * attempts side by side — agent, model, run status, diff stat and the patch
 * itself — plus the two decisions only a human makes: keep one attempt, or
 * drop the whole race. Both cancel the other attempts server-side, so nothing
 * here is optimistic: the list is re-read from the answer.
 */
export function RunGroupSection({ issueId, canManage = true }: { issueId: string; canManage?: boolean }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { data, isPending, isError } = useQuery(runGroupsOptions(wsId, issueId));
  const { data: agents = [] } = useQuery({ ...agentListOptions(wsId), enabled: canManage });
  const settle = useSettleRunGroup(wsId, issueId);
  const abandon = useAbandonRunGroup(wsId, issueId);
  const [startOpen, setStartOpen] = useState(false);
  const [confirm, setConfirm] = useState<
    { kind: "keep"; groupId: string; taskId: string; agent: string } | { kind: "abandon"; groupId: string } | null
  >(null);

  const groups = sortRunGroups(data ?? []);
  const canStart = canStartRunGroup(groups);
  const agentName = (id: string) => agents.find((a) => a.id === id)?.name ?? id.slice(0, 8);

  // A stale list is exactly what the two 409s mean, so re-read on either.
  const fail = (err: unknown) => {
    const kind = runGroupErrorKind(err);
    if (kind === "generic") {
      toast.error(err instanceof Error && err.message ? err.message : t(($) => $.race.failed));
      return;
    }
    toast.error(kind === "already_active" ? t(($) => $.race.error_already_active) : t(($) => $.race.error_already_settled));
    qc.invalidateQueries({ queryKey: runGroupKeys.issue(wsId, issueId) });
  };

  const runConfirmed = () => {
    if (!confirm) return;
    const done = { onError: fail, onSettled: () => setConfirm(null) };
    if (confirm.kind === "keep") settle.mutate({ groupId: confirm.groupId, winnerTaskId: confirm.taskId }, done);
    else abandon.mutate({ groupId: confirm.groupId }, done);
  };

  // Nothing to say to a reader who cannot start one and has none to read.
  if (!canManage && groups.length === 0) return null;

  return (
    <div data-testid="run-group-section" className="flex flex-col gap-2 text-caption">
      <div className="flex items-center gap-2 font-medium">
        <Flag className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
        <span>{t(($) => $.race.section)}</span>
        {canManage && (
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="ml-auto"
            disabled={!canStart}
            onClick={() => setStartOpen(true)}
          >
            {t(($) => $.race.start)}
          </Button>
        )}
      </div>

      {isPending ? (
        <p data-testid="run-group-loading" className="text-muted-foreground">{t(($) => $.race.loading)}</p>
      ) : isError ? (
        <p data-testid="run-group-error" className="text-destructive">{t(($) => $.race.load_failed)}</p>
      ) : groups.length === 0 ? (
        <p data-testid="run-group-empty" className="text-muted-foreground">{t(($) => $.race.empty)}</p>
      ) : (
        groups.map((group) => (
          <RunGroupCard
            key={group.id}
            group={group}
            agentName={agentName}
            canManage={canManage}
            busy={settle.isPending || abandon.isPending}
            onKeep={(attempt) => setConfirm({ kind: "keep", groupId: group.id, taskId: attempt.task_id, agent: agentName(attempt.agent_id) })}
            onAbandon={() => setConfirm({ kind: "abandon", groupId: group.id })}
          />
        ))
      )}

      {canManage && (
        <RunGroupStartDialog
          open={startOpen}
          onOpenChange={setStartOpen}
          issueId={issueId}
          wsId={wsId}
          agents={agents}
          onError={fail}
        />
      )}

      <AlertDialog open={confirm !== null} onOpenChange={(open) => { if (!open) setConfirm(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {confirm?.kind === "abandon" ? t(($) => $.race.abandon_title) : t(($) => $.race.keep_title, { agent: confirm?.kind === "keep" ? confirm.agent : "" })}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {confirm?.kind === "abandon" ? t(($) => $.race.abandon_desc) : t(($) => $.race.keep_desc)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.race.cancel)}</AlertDialogCancel>
            <AlertDialogAction
              onClick={runConfirmed}
              className={cn(confirm?.kind === "abandon" && "bg-destructive text-destructive-foreground hover:bg-destructive/90")}
            >
              {confirm?.kind === "abandon" ? t(($) => $.race.abandon_confirm) : t(($) => $.race.keep_confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function RunGroupCard({
  group,
  agentName,
  canManage,
  busy,
  onKeep,
  onAbandon,
}: {
  group: RunGroup;
  agentName: (id: string) => string;
  canManage: boolean;
  busy: boolean;
  onKeep: (attempt: RunGroupAttempt) => void;
  onAbandon: () => void;
}) {
  const { t } = useT("issues");
  const running = group.status === "running";
  return (
    <div
      data-testid="run-group"
      data-group-id={group.id}
      data-status={group.status}
      className={cn("flex flex-col gap-1.5 rounded-md border p-2", running ? "border-warning/60" : "border-border")}
    >
      <div className="flex items-center gap-2">
        <span
          className={cn(
            "rounded px-1",
            group.status === "settled" ? "bg-success/15 text-success" : running ? "bg-warning/20 text-warning" : "bg-muted text-muted-foreground",
          )}
        >
          {t(($) => $.race.status[group.status])}
        </span>
        <span className="text-muted-foreground">{t(($) => $.race.attempts, { count: group.attempts.length })}</span>
        {running && canManage && (
          <Button type="button" size="sm" variant="ghost" className="ml-auto" disabled={busy} onClick={onAbandon}>
            {t(($) => $.race.abandon)}
          </Button>
        )}
      </div>
      {/* The attempts scroll sideways inside this box; the issue page never does. */}
      <div className="overflow-x-auto">
        <div className="flex gap-2">
          {group.attempts.map((attempt) => (
            <AttemptColumn
              key={attempt.task_id}
              attempt={attempt}
              name={agentName(attempt.agent_id)}
              winner={group.winner_task_id === attempt.task_id}
              canKeep={running && canManage}
              busy={busy}
              onKeep={() => onKeep(attempt)}
            />
          ))}
        </div>
      </div>
    </div>
  );
}

function AttemptColumn({
  attempt,
  name,
  winner,
  canKeep,
  busy,
  onKeep,
}: {
  attempt: RunGroupAttempt;
  name: string;
  winner: boolean;
  canKeep: boolean;
  busy: boolean;
  onKeep: () => void;
}) {
  const { t } = useT("issues");
  const statusLabel = useStatusLabel(attempt.status);
  const stat = diffStatLabel(attempt.diff_stat);
  return (
    <div
      data-testid="run-group-attempt"
      data-task-id={attempt.task_id}
      data-winner={winner ? "true" : "false"}
      className={cn("flex w-64 shrink-0 flex-col gap-1 rounded-md border p-2", winner ? "border-success/60" : "border-border")}
    >
      <div className="flex items-center gap-1.5 font-medium">
        <TaskStatusIcon status={attempt.status} />
        <span className="truncate" title={name}>{name}</span>
        {winner && <span className="ml-auto shrink-0 text-success">{t(($) => $.race.kept)}</span>}
      </div>
      <dl className="grid grid-cols-[auto_1fr] gap-x-2 text-muted-foreground">
        <dt>{t(($) => $.race.model)}</dt>
        <dd className="truncate font-mono">{attempt.model || t(($) => $.race.default_model)}</dd>
        <dt>{t(($) => $.race.run_status)}</dt>
        <dd className="truncate">{statusLabel}</dd>
        <dt>{t(($) => $.race.diff_stat)}</dt>
        <dd className="font-mono">{stat ?? "—"}</dd>
      </dl>
      <AttemptDiff attempt={attempt} />
      {canKeep && (
        <Button type="button" size="sm" variant="outline" disabled={busy} onClick={onKeep}>
          {t(($) => $.race.keep, { agent: name })}
        </Button>
      )}
    </div>
  );
}

/**
 * The attempt's patch. `diff_truncated` is the case where the run did change
 * things and the patch was too large to store — saying "no changes" there
 * would be a lie, so it gets its own sentence.
 */
function AttemptDiff({ attempt }: { attempt: RunGroupAttempt }) {
  const { t } = useT("issues");
  if (attempt.diff_truncated) {
    return <p data-testid="run-group-diff-truncated" className="text-muted-foreground">{t(($) => $.race.diff_truncated)}</p>;
  }
  if (!attempt.diff_unified) {
    return <p className="text-muted-foreground">{t(($) => $.race.diff_none)}</p>;
  }
  return (
    <pre data-testid="run-group-diff" className="max-h-64 overflow-auto rounded bg-muted/40 p-2 font-mono text-caption">
      {diffUnifiedLines(attempt.diff_unified).map((line, i) => (
        <div
          key={i}
          className={cn(
            "whitespace-pre",
            line.kind === "added" && "bg-success/15",
            line.kind === "removed" && "bg-destructive/15",
            (line.kind === "hunk" || line.kind === "meta") && "text-muted-foreground",
          )}
        >
          {line.text || " "}
        </div>
      ))}
    </pre>
  );
}
