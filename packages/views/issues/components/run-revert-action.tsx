"use client";

import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, Undo2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { issueKeys } from "@multica/core/issues/queries";
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
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { useT } from "../../i18n";

// Revert this issue's worktree branch to the turn a run delivered (F09).
//
// Absent, never disabled, when the run cannot be reverted to. `revertable` is
// server-derived and fails closed, so a backend that predates the feature and a
// run with no checkpoint produce the same thing: no button. A disabled button
// would instead promise an affordance that can never become available on this
// row, and invite a click that explains nothing.
//
// The work itself happens on the user's own machine, so this is a request the
// UI follows rather than a mutation that returns an answer. The terminal
// states are the only interesting ones: `done` means the runs after this turn
// are gone, `failed` carries the daemon's named cause — a branch that moved,
// a checkpoint that was gc'd — which is shown verbatim because it is the only
// thing that tells the user what to do next.

const TERMINAL_STATUSES = new Set(["done", "failed"]);

export function RunRevertAction({
  task,
  issueId,
  laterRunCount,
}: {
  task: AgentTask;
  issueId: string;
  /** Runs after this turn on the same conversation — what the revert removes. */
  laterRunCount: number;
}) {
  const { t } = useT("issues");
  const qc = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const [requestId, setRequestId] = useState<string | null>(null);
  const [starting, setStarting] = useState(false);

  // Explicit `=== true`: `revertable` is a server field, and anything that is
  // not literally true — absent, malformed, a newer enum — must read as "no".
  const available = task.revertable === true;

  const { data: request } = useQuery({
    queryKey: ["issues", "run-revert", issueId, task.id, requestId ?? ""],
    queryFn: () => api.getRunRevertRequest(issueId, task.id, requestId as string),
    enabled: requestId !== null,
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status && TERMINAL_STATUSES.has(status) ? false : 1500;
    },
  });

  const settled =
    request?.status !== undefined && TERMINAL_STATUSES.has(request.status);
  const pending = starting || (requestId !== null && !settled);

  // One responder for the outcome: the poll's terminal transition. The
  // websocket `task:reverted` refreshes every client's run list, but only this
  // one asked for it, so only this one reports what happened.
  //
  // In an effect rather than during render: reporting is a side effect, and a
  // toast fired from the render path would repeat on every re-render the poll
  // causes.
  useEffect(() => {
    if (requestId === null || !settled) return;
    setRequestId(null);
    if (request?.status === "done") {
      toast.success(t(($) => $.execution_log.revert_done));
      void qc.invalidateQueries({ queryKey: issueKeys.tasks(issueId) });
    } else {
      toast.error(request?.error || t(($) => $.execution_log.revert_failed));
    }
  }, [requestId, settled, request?.status, request?.error, issueId, qc, t]);

  if (!available) return null;

  const handleConfirm = async () => {
    setStarting(true);
    try {
      const created = await api.requestRunRevert(issueId, task.id);
      if (!created.request_id) {
        toast.error(t(($) => $.execution_log.revert_failed));
        return;
      }
      setRequestId(created.request_id);
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.execution_log.revert_failed),
      );
    } finally {
      setStarting(false);
    }
  };

  return (
    <>
      <Tooltip>
        <TooltipTrigger
          render={
            <button
              type="button"
              onClick={() => setConfirming(true)}
              disabled={pending}
              aria-label={t(($) => $.execution_log.revert_aria)}
            />
          }
          className="flex items-center justify-center rounded p-1 text-muted-foreground transition-colors hover:bg-accent/50 hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
        >
          {pending ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Undo2 className="h-3.5 w-3.5" />
          )}
        </TooltipTrigger>
        <TooltipContent>
          {pending
            ? t(($) => $.execution_log.revert_pending)
            : t(($) => $.execution_log.revert_tooltip)}
        </TooltipContent>
      </Tooltip>

      {confirming && (
        <AlertDialog open onOpenChange={setConfirming}>
          {/* The dialog can render inside a clickable row; keep its clicks
              from reaching it. */}
          <AlertDialogContent onClick={(e) => e.stopPropagation()}>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {t(($) => $.execution_log.revert_dialog_title)}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {laterRunCount > 0
                  ? t(($) => $.execution_log.revert_dialog_body, { count: laterRunCount })
                  : t(($) => $.execution_log.revert_dialog_body_none)}{" "}
                {t(($) => $.execution_log.revert_dialog_note)}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>
                {t(($) => $.execution_log.revert_dialog_keep)}
              </AlertDialogCancel>
              <AlertDialogAction
                variant="destructive"
                onClick={() => {
                  setConfirming(false);
                  void handleConfirm();
                }}
              >
                {t(($) => $.execution_log.revert_dialog_confirm)}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}
    </>
  );
}
