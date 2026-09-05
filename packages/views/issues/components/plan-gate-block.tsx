"use client";

import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { isPlanMaterialized, planStepStages, useMaterializeIssuePlan } from "@multica/core/issues/plan";
import type { IssuePlan } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT, useTimeAgo } from "../../i18n";

/**
 * Plan Gate (K11): the steps a plan version would create as sub-issues, with
 * their stage, and the approval that creates them. Once materialized, the
 * steps point at their sub-issues; the children list on the issue shows them.
 */
export function PlanGateBlock({ issueId, plan }: { issueId: string; plan: IssuePlan }) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const materialize = useMaterializeIssuePlan(wsId);
  const materialized = isPlanMaterialized(plan);
  const stages = planStepStages(plan.steps);

  if (plan.steps.length === 0) {
    return <div className="text-muted-foreground">{t(($) => $.plan_verification.gate_no_steps)}</div>;
  }

  return (
    <div data-testid="plan-gate" data-materialized={materialized ? "true" : "false"} className="flex flex-col gap-1">
      <ol className="flex flex-col gap-0.5">
        {plan.steps.map((s) => (
          <li key={s.id} className="flex items-center gap-2">
            <span className="shrink-0 font-mono text-faint-foreground">{t(($) => $.plan_verification.gate_stage, { stage: stages.get(s.id) ?? 1 })}</span>
            <span className={cn("min-w-0 flex-1 truncate", s.issue_id ? "text-muted-foreground" : "")} title={s.title}>
              {s.title}
            </span>
            {s.issue_id && <span className="shrink-0 text-success">{t(($) => $.plan_verification.gate_created)}</span>}
          </li>
        ))}
      </ol>
      {materialized ? (
        <div className="text-muted-foreground">
          {t(($) => $.plan_verification.gate_materialized, { count: plan.steps.length })}
          {plan.materialized_at && <span> · {timeAgo(plan.materialized_at)}</span>}
        </div>
      ) : plan.superseded_at ? (
        <div className="text-muted-foreground">{t(($) => $.plan_verification.gate_superseded)}</div>
      ) : (
        <Button
          type="button"
          size="sm"
          className="self-start"
          disabled={materialize.isPending}
          onClick={() =>
            materialize.mutate(
              { issueId, version: plan.version },
              { onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.plan_verification.gate_failed)) },
            )
          }
        >
          {t(($) => $.plan_verification.gate_approve, { count: plan.steps.length })}
        </Button>
      )}
    </div>
  );
}
