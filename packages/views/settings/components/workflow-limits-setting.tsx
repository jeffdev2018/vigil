"use client";

import { useEffect, useState } from "react";
import { Repeat } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useSaveWorkflowLimits, workflowLimitsOptions } from "@multica/core/agents/routing-check";
import { Input } from "@multica/ui/components/ui/input";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

/**
 * Bounded workflows (JEF-275). A run is already bounded on its own; this is the
 * ceiling on the workflow it can spawn — review, revision, retry, contest —
 * which nothing else bounds.
 */
export function WorkflowLimitsSetting({ canEdit }: { canEdit: boolean }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data: limits } = useQuery(workflowLimitsOptions(wsId));
  const save = useSaveWorkflowLimits(wsId);
  const [legs, setLegs] = useState("8");
  const [cost, setCost] = useState("0");
  useEffect(() => {
    if (!limits) return;
    setLegs(String(limits.max_legs));
    setCost(String(limits.max_cost_usd_ticks));
  }, [limits]);

  const failed = (e: unknown) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.workspace.workflow_limits_failed));

  const commitLegs = () => {
    const lo = limits?.min_legs ?? 1;
    const hi = limits?.max_legs_allowed ?? 50;
    const n = Math.max(lo, Math.min(hi, Math.floor(Number(legs) || 0)));
    setLegs(String(n));
    if (!limits || n === limits.max_legs) return;
    save.mutate({ max_legs: n, max_cost_usd_ticks: limits.max_cost_usd_ticks }, { onError: failed });
  };

  const commitCost = () => {
    const n = Math.max(0, Math.floor(Number(cost) || 0));
    setCost(String(n));
    if (!limits || n === limits.max_cost_usd_ticks) return;
    save.mutate({ max_legs: limits.max_legs, max_cost_usd_ticks: n }, { onError: failed });
  };

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <Repeat className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.workflow_limits_section)}
        </span>
      }
    >
      <SettingsCard>
        <SettingsRow label={t(($) => $.workspace.workflow_max_legs)} description={t(($) => $.workspace.workflow_max_legs_description)}>
          <Input
            type="number"
            min={limits?.min_legs ?? 1}
            max={limits?.max_legs_allowed ?? 50}
            aria-label={t(($) => $.workspace.workflow_max_legs)}
            className="w-24"
            value={legs}
            disabled={!canEdit || save.isPending}
            onChange={(e) => setLegs(e.target.value)}
            onBlur={commitLegs}
          />
        </SettingsRow>
        <SettingsRow label={t(($) => $.workspace.workflow_max_cost)} description={t(($) => $.workspace.workflow_max_cost_description)}>
          <Input
            type="number"
            min={0}
            aria-label={t(($) => $.workspace.workflow_max_cost)}
            className="w-32"
            value={cost}
            disabled={!canEdit || save.isPending}
            onChange={(e) => setCost(e.target.value)}
            onBlur={commitCost}
          />
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}
