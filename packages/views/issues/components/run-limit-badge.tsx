"use client";

import { useQuery } from "@tanstack/react-query";
import { Gauge } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { formatGateValue, issueRunLimitEventsOptions } from "@multica/core/budgets/run-limits";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

/**
 * Run limits (K03): the latest run's limit events — a warning, an observed
 * overrun, or the stop that ended it — with the gate and the numbers.
 * Renders nothing until a run crossed a threshold.
 */
export function RunLimitBadge({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: events = [] } = useQuery(issueRunLimitEventsOptions(wsId, issueId));
  if (events.length === 0) return null;
  const latestTask = events[0]?.task_id;
  const shown = events.filter((e) => e.task_id === latestTask);
  const stopped = shown.some((e) => e.level === "stopped");
  return (
    <div data-testid="run-limit-badge" data-stopped={stopped ? "true" : "false"} className={cn("flex flex-wrap items-center gap-2 rounded-md border px-2 py-1 text-caption", stopped ? "border-destructive/50 bg-destructive/10" : "border-warning/50 bg-warning/10")}>
      <Gauge className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
      <span className="font-medium">{stopped ? t(($) => $.run_limits.stopped) : t(($) => $.run_limits.title)}</span>
      {shown.map((e, i) => (
        <span key={i} className="font-mono">
          {t(($) => $.run_limits.gates[e.gate])} {formatGateValue(e.gate, e.observed)} / {formatGateValue(e.gate, e.limit)} · {t(($) => $.run_limits.levels[e.level])}
        </span>
      ))}
    </div>
  );
}
