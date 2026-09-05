"use client";

import { Wrench } from "lucide-react";
import { toast } from "sonner";
import { ciAutoFixState, useRetryCIAutoFix, type IssueCIAutoFix } from "@multica/core/issues/ci-auto-fix";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

/**
 * CI auto-fix (K49): the state of the correction runs on one pull request —
 * in progress, fixed, failed, or exhausted with a manual retry. Nothing
 * when the workspace switch is off and no run ever happened.
 */
export function CIAutoFixChip({ issueId, wsId, pullRequestId, data }: { issueId: string; wsId: string; pullRequestId: string; data: IssueCIAutoFix | undefined }) {
  const { t } = useT("issues");
  const retry = useRetryCIAutoFix(wsId, issueId);
  if (!data) return null;
  const { state, attempts } = ciAutoFixState(data.runs, pullRequestId, data.max_attempts);
  if (state === "none") return null;
  const tone = state === "fixed" ? "bg-success/15 text-success" : state === "in_progress" ? "bg-info/15 text-info animate-pulse" : "bg-destructive/15 text-destructive";
  return (
    <span data-testid="ci-auto-fix-chip" data-state={state} className="inline-flex items-center gap-1 text-micro">
      <span className={cn("inline-flex items-center gap-1 rounded px-1", tone)}>
        <Wrench className="h-3 w-3" aria-hidden="true" />
        {t(($) => $.ci_auto_fix[state], { count: attempts })}
      </span>
      {state === "exhausted" && (
        <button type="button" className="text-muted-foreground underline hover:text-foreground" disabled={retry.isPending} onClick={(e) => { e.preventDefault(); e.stopPropagation(); retry.mutate(pullRequestId, { onError: (err) => toast.error(err instanceof Error && err.message ? err.message : t(($) => $.ci_auto_fix.retry_failed)) }); }}>
          {t(($) => $.ci_auto_fix.retry)}
        </button>
      )}
    </span>
  );
}
