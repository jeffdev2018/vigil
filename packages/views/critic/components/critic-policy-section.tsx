"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions } from "@multica/core/workspace/queries";
import {
  criticCostTicks,
  criticCostUsd,
  criticPolicyError,
  criticPolicyOptions,
  useSaveCriticPolicy,
  type CriticPolicyWrite,
} from "@multica/core/critic";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import { useT } from "../../i18n";

/**
 * Adversarial review (F25): the per-agent / per-squad critic policy.
 *
 * The one rule the form enforces itself is the one the server answers 422 for:
 * an enabled policy names its critic, and an agent is never its own. Saving is
 * refused inline rather than round-tripping, because the toggle otherwise
 * looks accepted for the length of a request.
 */
export function CriticPolicySection({
  subjectType,
  subjectId,
  canManage = true,
}: {
  subjectType: "agent" | "squad";
  subjectId: string;
  canManage?: boolean;
}) {
  const { t } = useT("critic");
  const wsId = useWorkspaceId();
  const { data: policy } = useQuery(criticPolicyOptions(wsId, subjectType, subjectId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const save = useSaveCriticPolicy(wsId, subjectType, subjectId);

  const [draft, setDraft] = useState<CriticPolicyWrite | null>(null);
  const [budget, setBudget] = useState("");
  useEffect(() => {
    if (!policy) return;
    setDraft({
      enabled: policy.enabled,
      critic_agent_id: policy.critic_agent_id ?? "",
      require_distinct_provider: policy.require_distinct_provider,
      blocking: policy.blocking,
      max_rounds: policy.max_rounds,
    });
    setBudget(criticCostUsd(policy.max_cost_usd_ticks).replace("$", ""));
  }, [policy]);

  if (!draft) return null;
  const patch = (next: Partial<CriticPolicyWrite>) => setDraft({ ...draft, ...next });
  const error = criticPolicyError(draft, subjectType, subjectId);
  const candidates = agents.filter(
    (a) => !a.archived_at && !(subjectType === "agent" && a.id === subjectId),
  );

  return (
    <section data-testid="critic-policy" data-enabled={draft.enabled} className="mt-4 flex flex-col gap-2 text-caption">
      <div>
        <h3 className="font-medium">{t(($) => $.section.title)}</h3>
        <p className="text-muted-foreground">
          {subjectType === "squad" ? t(($) => $.section.description_squad) : t(($) => $.section.description)}
        </p>
      </div>

      <div className="flex items-center gap-2">
        <Switch
          id="critic-enabled"
          data-testid="critic-enabled"
          checked={draft.enabled}
          disabled={!canManage}
          onCheckedChange={(v: boolean) => patch({ enabled: v })}
        />
        <Label htmlFor="critic-enabled">{t(($) => $.section.enabled)}</Label>
      </div>

      {draft.enabled && (
        <div className="flex flex-col gap-2">
          <div className="flex flex-col gap-1">
            <Label htmlFor="critic-agent">{t(($) => $.section.critic)}</Label>
            <select
              id="critic-agent"
              data-testid="critic-agent"
              className="h-8 rounded-md border border-input bg-background px-2 text-caption"
              value={draft.critic_agent_id ?? ""}
              disabled={!canManage}
              onChange={(e) => patch({ critic_agent_id: e.target.value })}
            >
              <option value="">{t(($) => $.section.critic_placeholder)}</option>
              {candidates.map((a) => (
                <option key={a.id} value={a.id}>{a.name}</option>
              ))}
            </select>
          </div>

          <div className="flex items-center gap-2">
            <Switch
              id="critic-distinct"
              data-testid="critic-distinct"
              checked={draft.require_distinct_provider ?? true}
              disabled={!canManage}
              onCheckedChange={(v: boolean) => patch({ require_distinct_provider: v })}
            />
            <Label htmlFor="critic-distinct">{t(($) => $.section.require_distinct_provider)}</Label>
          </div>
          <p className="text-muted-foreground">{t(($) => $.section.require_distinct_provider_hint)}</p>

          <div className="flex items-center gap-2">
            <Switch
              id="critic-blocking"
              data-testid="critic-blocking"
              checked={draft.blocking ?? false}
              disabled={!canManage}
              onCheckedChange={(v: boolean) => patch({ blocking: v })}
            />
            <Label htmlFor="critic-blocking">{t(($) => $.section.blocking)}</Label>
          </div>
          <p className="text-muted-foreground">{t(($) => $.section.blocking_hint)}</p>

          <div className="flex flex-wrap gap-3">
            <div className="flex flex-col gap-1">
              <Label htmlFor="critic-rounds">{t(($) => $.section.max_rounds)}</Label>
              <Input
                id="critic-rounds"
                data-testid="critic-rounds"
                type="number"
                min={1}
                max={10}
                className="h-8 w-20"
                value={draft.max_rounds ?? 1}
                disabled={!canManage}
                onChange={(e) => patch({ max_rounds: Number.parseInt(e.target.value, 10) || 1 })}
              />
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor="critic-budget">{t(($) => $.section.budget)}</Label>
              <Input
                id="critic-budget"
                data-testid="critic-budget"
                inputMode="decimal"
                className="h-8 w-28"
                placeholder={t(($) => $.section.budget_placeholder)}
                value={budget}
                disabled={!canManage}
                onChange={(e) => setBudget(e.target.value)}
              />
            </div>
          </div>
          <p className="text-muted-foreground">{t(($) => $.section.max_rounds_hint)} {t(($) => $.section.budget_hint)}</p>
        </div>
      )}

      {error !== "" && (
        <p data-testid="critic-error" role="alert" className="text-destructive">
          {error === "critic_is_author"
            ? t(($) => $.section.error_critic_is_author)
            : t(($) => $.section.error_critic_required)}
        </p>
      )}
      {save.isError && (
        <p data-testid="critic-save-error" role="alert" className="text-destructive">{t(($) => $.section.error_save)}</p>
      )}

      {canManage && (
        <div>
          <Button
            size="sm"
            data-testid="critic-save"
            disabled={error !== "" || save.isPending}
            onClick={() =>
              save.mutate({
                ...draft,
                critic_agent_id: draft.critic_agent_id || undefined,
                max_cost_usd_ticks: criticCostTicks(budget),
              })
            }
          >
            {save.isPending ? t(($) => $.section.saving) : t(($) => $.section.save)}
          </Button>
        </div>
      )}
    </section>
  );
}
