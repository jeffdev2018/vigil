"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, CheckCircle2, XCircle } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentRoutingCheckOptions, routingCheckSummary } from "@multica/core/agents/routing-check";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

/**
 * Validated routing (JEF-275): would a trigger for this agent actually run?
 *
 * The two tones are not decoration. An error is a problem no amount of waiting
 * resolves — the trigger is refused outright — while a warning means the run is
 * queued and waiting on something that comes back by itself.
 */
export function AgentRoutingCheck({ agentId }: { agentId: string }) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const { data } = useQuery(agentRoutingCheckOptions(wsId, agentId));
  const summary = routingCheckSummary(data);
  if (!summary) return null;
  const Icon = summary.tone === "error" ? XCircle : summary.tone === "warning" ? AlertTriangle : CheckCircle2;
  return (
    <section data-testid="agent-routing-check" data-tone={summary.tone} className="mt-4 text-caption">
      <h3 className="mb-1 font-medium">{t(($) => $.routing_check.title)}</h3>
      <p
        className={cn(
          "flex items-start gap-1.5",
          summary.tone === "error" && "text-destructive",
          summary.tone === "warning" && "text-muted-foreground",
          summary.tone === "ok" && "text-muted-foreground",
        )}
      >
        <Icon className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden />
        <span>
          {summary.tone === "ok" ? t(($) => $.routing_check.ok) : summary.message}
          {summary.extra > 0 && <span className="ml-1 text-muted-foreground">{t(($) => $.routing_check.more, { count: summary.extra })}</span>}
        </span>
      </p>
    </section>
  );
}
