"use client";

import { useQuery } from "@tanstack/react-query";
import { RotateCcw, ServerCrash } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueRunCheckpointOptions } from "@multica/core/issues/checkpoint";
import { cn } from "@multica/ui/lib/utils";
import { useT, useTimeAgo } from "../../i18n";

/**
 * Checkpoints (K20): tells the human when the latest run was interrupted
 * and resumed automatically from its checkpoint, and when the resume chain
 * gave up — an infrastructure failure, not the task failing on its own.
 * Renders nothing while no interruption happened.
 */
export function RunInterruptedBanner({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const { data: run } = useQuery(issueRunCheckpointOptions(wsId, issueId));
  if (!run || (run.attempts === 0 && !run.exhausted)) return null;
  const point = run.last_checkpoint_seq != null ? t(($) => $.checkpoint.from_seq, { seq: run.last_checkpoint_seq }) : t(($) => $.checkpoint.from_start);
  return (
    <div
      data-testid="run-interrupted-banner"
      data-exhausted={run.exhausted ? "true" : "false"}
      className={cn("flex flex-wrap items-center gap-2 rounded-md border px-2 py-1.5 text-caption", run.exhausted ? "border-destructive/50 bg-destructive/10" : "border-info/50 bg-info/10")}
    >
      {run.exhausted ? <ServerCrash className="h-3.5 w-3.5 shrink-0" aria-hidden="true" /> : <RotateCcw className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />}
      <span className="font-medium">
        {run.exhausted ? t(($) => $.checkpoint.exhausted, { attempts: run.attempts, max: run.max_attempts }) : t(($) => $.checkpoint.resumed, { attempts: run.attempts, max: run.max_attempts, point })}
      </span>
      {run.checkpointed_at && <span className="text-muted-foreground">{t(($) => $.checkpoint.checkpointed, { when: timeAgo(run.checkpointed_at) })}</span>}
    </div>
  );
}
