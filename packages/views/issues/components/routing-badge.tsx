"use client";

import { useQuery } from "@tanstack/react-query";
import { Route, TrendingUp } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueRoutingOptions, type RiskLevel } from "@multica/core/issues/routing";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

const RISK_TONE: Record<RiskLevel, string> = {
  low: "text-success",
  normal: "text-muted-foreground",
  high: "text-warning",
};

/**
 * Issue router (K27): the decision behind the latest run — risk level, the
 * pool it went to, and a distinct mark with the reason when it escalated.
 * Renders nothing until a run was routed.
 */
export function RoutingBadge({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data } = useQuery(issueRoutingOptions(wsId, issueId));
  const d = data?.decision;
  if (!d) return null;
  return (
    <div data-testid="routing-badge" data-escalated={d.escalated ? "true" : "false"} className="flex flex-wrap items-center gap-2 px-2 py-1 text-caption">
      <Route className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
      <span className="font-medium">{t(($) => $.routing.label)}</span>
      <span className={cn("rounded border border-border px-1", RISK_TONE[d.risk_level] ?? RISK_TONE.normal)}>{t(($) => $.routing.risk[d.risk_level] ?? $.routing.risk.normal)}</span>
      <span className="text-muted-foreground">{d.target_pool_name ? t(($) => $.routing.pool, { name: d.target_pool_name }) : t(($) => $.routing.agent_pool)}</span>
      {d.matched_paths.length > 0 && <span className="font-mono text-muted-foreground" title={d.matched_paths.join(", ")}>{t(($) => $.routing.paths, { count: d.matched_paths.length })}</span>}
      {d.escalated && (
        <span className="inline-flex items-center gap-1 rounded bg-warning/20 px-1 font-medium text-warning" title={d.escalation_reason}>
          <TrendingUp className="h-3 w-3" aria-hidden="true" />
          {t(($) => $.routing.escalated)}
          {d.escalation_reason && <span className="font-normal">· {d.escalation_reason}</span>}
        </span>
      )}
    </div>
  );
}
