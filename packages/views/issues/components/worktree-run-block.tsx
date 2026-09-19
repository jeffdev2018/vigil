"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, GitBranch, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { isRunSettled, runStateOf } from "@multica/core/agents/run-state";
import { issueTasksOptions } from "@multica/core/issues/queries";
import {
  canDiscardRun,
  canPromoteRun,
  runBranchActionErrorKind,
  runDiffOptions,
  useDiscardRun,
  usePromoteRun,
} from "@multica/core/issues/run-actions";
import { diffStatLabel } from "@multica/core/issues/run-group";
import type { AgentTask } from "@multica/core/types";
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
import { UnifiedDiff } from "../../common/unified-diff";

// Worktree run branch (JEF-255): under a finished run's row, the branch the
// run delivered on, its recorded diff (fetched only when expanded), and the
// two ways to close the branch out — promote (push + open a PR) or discard
// (delete branch and worktree).
//
// Nothing here is optimistic: promote and discard are requests the daemon
// executes on the user's machine, so the POST only enqueues and the outcome
// is read back off the task row. While `pending_branch_action` is set this
// block polls the issue's task list (the section's own query — this observer
// only adds the interval) until the marker lands: `promoted_at` /
// `promote_pr_url` on success, `discarded_at` after a discard. A discarded
// run keeps only a muted marker — the branch is gone, so its diff and
// actions go with it.
export function WorktreeRunBlock({ task, issueId }: { task: AgentTask; issueId: string }) {
  const { t } = useT("issues");
  const [diffOpen, setDiffOpen] = useState(false);
  const [confirm, setConfirm] = useState<"promote" | "discard" | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const promote = usePromoteRun(issueId);
  const discard = useDiscardRun(issueId);

  const pendingAction =
    task.pending_branch_action === "promote" || task.pending_branch_action === "discard"
      ? task.pending_branch_action
      : null;

  useQuery({
    ...issueTasksOptions(issueId),
    refetchInterval: pendingAction !== null ? 1500 : false,
  });

  if (!task.branch_name || !isRunSettled(runStateOf(task.status))) return null;

  if (task.discarded_at) {
    return (
      <div className="flex items-center gap-1.5 pl-8 pr-1 pb-1 text-caption text-muted-foreground">
        <GitBranch className="h-3 w-3 shrink-0" aria-hidden="true" />
        <span className="min-w-0 truncate font-mono">{task.branch_name}</span>
        <span data-testid="run-discarded" className="shrink-0 rounded bg-muted px-1 py-px text-micro">
          {t(($) => $.execution_log.branch_discarded)}
        </span>
      </div>
    );
  }

  const promoted = Boolean(task.promoted_at);
  // A pending action disables both buttons, not just its own: the server
  // rejects a second one with 409 run_branch_action_pending anyway.
  const busy = pendingAction !== null || promote.isPending || discard.isPending;

  const fail = (err: unknown) => {
    const kind = runBranchActionErrorKind(err);
    if (kind === "generic") {
      toast.error(err instanceof Error && err.message ? err.message : t(($) => $.execution_log.branch_action_failed));
      return;
    }
    // Every 409 here means the row was stale — the list is already being
    // re-read (the mutation's onSettled invalidated it), so say so inline.
    setActionError(
      kind === "not_promotable"
        ? t(($) => $.execution_log.branch_error_not_promotable)
        : kind === "not_discardable"
          ? t(($) => $.execution_log.branch_error_not_discardable)
          : t(($) => $.execution_log.branch_error_pending),
    );
  };

  const runConfirmed = () => {
    if (!confirm) return;
    setActionError(null);
    const done = { onError: fail, onSettled: () => setConfirm(null) };
    if (confirm === "promote") promote.mutate({ taskId: task.id }, done);
    else discard.mutate({ taskId: task.id }, done);
  };

  return (
    <div data-testid="worktree-run-block" className="space-y-1 pl-8 pr-1 pb-1 text-caption">
      <div className="flex items-center gap-1.5">
        <GitBranch className="h-3 w-3 shrink-0 text-muted-foreground" aria-hidden="true" />
        <span className="min-w-0 truncate font-mono text-muted-foreground" title={task.branch_name}>
          {task.branch_name}
        </span>
        {promoted ? (
          task.promote_pr_url ? (
            <a
              data-testid="run-promoted"
              href={task.promote_pr_url}
              target="_blank"
              rel="noopener noreferrer"
              className="shrink-0 rounded bg-success/15 px-1 py-px text-micro text-success hover:bg-success/25"
            >
              {t(($) => $.execution_log.branch_promoted_view_pr)}
            </a>
          ) : (
            <span data-testid="run-promoted" className="shrink-0 rounded bg-success/15 px-1 py-px text-micro text-success">
              {t(($) => $.execution_log.branch_promoted)}
            </span>
          )
        ) : (
          <div className="ml-auto flex shrink-0 items-center gap-1">
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={busy || !canPromoteRun(task)}
              onClick={() => setConfirm("promote")}
            >
              {pendingAction === "promote" && <Loader2 className="animate-spin" />}
              {pendingAction === "promote"
                ? t(($) => $.execution_log.branch_promote_pending)
                : t(($) => $.execution_log.branch_promote)}
            </Button>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              className="text-destructive hover:bg-destructive/10 hover:text-destructive"
              disabled={busy || !canDiscardRun(task)}
              onClick={() => setConfirm("discard")}
            >
              {pendingAction === "discard" && <Loader2 className="animate-spin" />}
              {pendingAction === "discard"
                ? t(($) => $.execution_log.branch_discard_pending)
                : t(($) => $.execution_log.branch_discard)}
            </Button>
          </div>
        )}
      </div>

      <button
        type="button"
        onClick={() => setDiffOpen(!diffOpen)}
        aria-expanded={diffOpen}
        className="flex items-center gap-1 rounded px-1 py-0.5 text-muted-foreground transition-colors hover:bg-accent/40 hover:text-foreground"
      >
        <ChevronRight className={cn("!size-3 shrink-0 stroke-[2.5] transition-transform", diffOpen && "rotate-90")} />
        {diffOpen ? t(($) => $.execution_log.branch_hide_diff) : t(($) => $.execution_log.branch_show_diff)}
      </button>
      {diffOpen && <RunDiff taskId={task.id} />}

      {actionError !== null && (
        <p data-testid="run-branch-action-error" className="text-destructive">{actionError}</p>
      )}

      <AlertDialog open={confirm !== null} onOpenChange={(open) => { if (!open) setConfirm(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {confirm === "discard"
                ? t(($) => $.execution_log.branch_discard_dialog_title)
                : t(($) => $.execution_log.branch_promote_dialog_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {confirm === "discard"
                ? t(($) => $.execution_log.branch_discard_dialog_body, { branch: task.branch_name ?? "" })
                : t(($) => $.execution_log.branch_promote_dialog_body, { branch: task.branch_name ?? "" })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.execution_log.branch_dialog_cancel)}</AlertDialogCancel>
            <AlertDialogAction
              onClick={runConfirmed}
              className={cn(confirm === "discard" && "bg-destructive text-destructive-foreground hover:bg-destructive/90")}
            >
              {confirm === "discard"
                ? t(($) => $.execution_log.branch_discard_dialog_confirm)
                : t(($) => $.execution_log.branch_promote_dialog_confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

// The run's recorded patch, loaded only once the diff block is expanded. A
// 404 (run_diff_not_found) reads as "no diff recorded", the same sentence a
// null body gets — the run ended without a patch either way.
function RunDiff({ taskId }: { taskId: string }) {
  const { t } = useT("issues");
  const { data, isPending, isError } = useQuery(runDiffOptions(taskId, true));
  if (isPending) {
    return <p data-testid="run-diff-loading" className="text-muted-foreground">{t(($) => $.execution_log.branch_diff_loading)}</p>;
  }
  return (
    <div className="space-y-1">
      {!isError && data && diffStatLabel(data.diff_stat) !== null && (
        <span data-testid="run-diff-stat" className="font-mono text-muted-foreground">
          {diffStatLabel(data.diff_stat)}
        </span>
      )}
      <UnifiedDiff
        diff={isError ? null : (data?.diff_unified ?? null)}
        truncated={!isError && (data?.diff_truncated ?? false)}
        emptyLabel={t(($) => $.race.diff_none)}
        truncatedLabel={t(($) => $.race.diff_truncated)}
        testid="run-diff"
      />
    </div>
  );
}
