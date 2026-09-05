"use client";

import { useQuery } from "@tanstack/react-query";
import { PauseOctagon } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { issuePreemptionsOptions } from "@multica/core/issues/preemption";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../../navigation";
import { useT, useTimeAgo } from "../../i18n";

/**
 * Preemption (K41): this issue's run was suspended to let an urgent issue
 * go first. Names the issue, links to it, and says whether the run already
 * resumed. Renders nothing while no run was preempted.
 */
export function PreemptedBadge({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { data: preemptions = [] } = useQuery(issuePreemptionsOptions(wsId, issueId));
  if (preemptions.length === 0) return null;
  const latest = preemptions[0]!;
  const waiting = latest.resumed_by_task_id == null && latest.status === "paused";
  const identifier = latest.preempted_by_identifier ?? latest.preempted_by_task_id.slice(0, 8);
  return (
    <div data-testid="preempted-badge" data-waiting={waiting ? "true" : "false"} className={cn("flex flex-wrap items-center gap-2 rounded-md border px-2 py-1 text-caption", waiting ? "border-warning/50 bg-warning/10" : "border-border text-muted-foreground")}>
      <PauseOctagon className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
      <span className="font-medium">{waiting ? t(($) => $.preemption.waiting) : t(($) => $.preemption.resumed)}</span>
      <span>
        {t(($) => $.preemption.for)}{" "}
        {latest.preempted_by_issue_id ? <AppLink href={paths.issueDetail(latest.preempted_by_issue_id)} className="font-mono underline-offset-2 hover:underline">{identifier}</AppLink> : <span className="font-mono">{identifier}</span>}
      </span>
      {waiting && <span className="text-muted-foreground">{t(($) => $.preemption.estimate)}</span>}
      {preemptions.length > 1 && <span className="text-muted-foreground">{t(($) => $.preemption.count, { count: preemptions.length })}</span>}
      <span className="ml-auto text-muted-foreground">{timeAgo(latest.preempted_at)}</span>
    </div>
  );
}
