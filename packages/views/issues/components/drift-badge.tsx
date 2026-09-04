"use client";

import { useQuery } from "@tanstack/react-query";
import { Repeat } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueRunsOptions } from "@multica/core/issues/handoff";
import { useT } from "../../i18n";

/**
 * Drift detection (K40): the exact reason the latest run was stopped for
 * going in circles, on the issue itself, not only in logs. Renders nothing
 * while no run drifted.
 */
export function DriftBadge({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  useWorkspaceId();
  const { data: runs = [] } = useQuery(issueRunsOptions(issueId));
  const drifted = runs.find((r) => r.drift_reason === "repeated_action" || r.drift_reason === "file_reread_loop");
  if (!drifted) return null;
  const reason = drifted.drift_reason as "repeated_action" | "file_reread_loop";
  return (
    <div data-testid="drift-badge" data-reason={reason} className="flex flex-wrap items-center gap-2 rounded-md border border-destructive/50 bg-destructive/10 px-2 py-1 text-caption">
      <Repeat className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
      <span className="font-medium">{t(($) => $.drift.stopped)}</span>
      <span>{t(($) => $.drift.reasons[reason])}</span>
      {drifted.error && <span className="text-muted-foreground">· {drifted.error}</span>}
      <span className="ml-auto font-mono text-muted-foreground">{t(($) => $.drift.run, { id: drifted.id.slice(0, 8) })}</span>
    </div>
  );
}
