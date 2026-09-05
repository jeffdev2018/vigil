"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { busiestHours, formatReviewLoad, ruleSummary, useForgetObservation, useSetObservationAuto, workProfileOptions } from "@multica/core/work-profile";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { cn } from "@multica/ui/lib/utils";
import { useT, useTimeAgo } from "../../i18n";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";

/**
 * "What Vigil knows about me" (K71): every observation learned from my own
 * decisions, with its source and date, a switch to let a rule decide alone
 * (never for high stakes), a forget button, and the review load my decisions
 * cost me — counted, not only the time saved.
 */
export function LearningTab() {
  const { t } = useT("settings");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const { data, isPending } = useQuery(workProfileOptions(wsId));
  const setAuto = useSetObservationAuto(wsId);
  const forget = useForgetObservation(wsId);
  const fail = (e: unknown, fallback: string) => toast.error(e instanceof Error && e.message ? e.message : fallback);
  const rules = (data?.observations ?? []).filter((o) => o.kind === "decision_rule");
  const hours = (data?.observations ?? []).find((o) => o.key === "decision_hour");

  return (
    <div data-testid="learning-tab" className="flex flex-col gap-6">
      <SettingsSection title={t(($) => $.learning.title)} description={t(($) => $.learning.intro)}>
        <SettingsCard>
          <SettingsRow label={t(($) => $.learning.examples)} description={t(($) => $.learning.examples_description)}>
            <span data-testid="learning-examples" className="tabular-nums">{data?.examples ?? 0}</span>
          </SettingsRow>
          <SettingsRow label={t(($) => $.learning.review_load)} description={t(($) => $.learning.review_load_description)}>
            <span className="tabular-nums">{formatReviewLoad(data?.review_load_seconds ?? 0)}</span>
          </SettingsRow>
          <SettingsRow label={t(($) => $.learning.auto_decided)} description={t(($) => $.learning.auto_decided_description, { overturned: data?.overturned ?? 0 })}>
            <span className="tabular-nums">{data?.auto_decided ?? 0}</span>
          </SettingsRow>
          <SettingsRow label={t(($) => $.learning.surface)} description={t(($) => $.learning.surface_description)}>
            <span className="text-muted-foreground">{(data?.adaptation_surface ?? []).map((s) => t(($) => $.learning.surfaces[s as "decision_rules" | "decision_hours"] ?? s)).join(" · ") || "–"}</span>
          </SettingsRow>
        </SettingsCard>
      </SettingsSection>

      <SettingsSection title={t(($) => $.learning.rules)} description={t(($) => $.learning.rules_description)}>
        <SettingsCard>
          {isPending && <p className="p-3 text-caption text-muted-foreground">{t(($) => $.learning.loading)}</p>}
          {!isPending && rules.length === 0 && <p data-testid="learning-empty" className="p-3 text-caption text-muted-foreground">{t(($) => $.learning.empty)}</p>}
          {rules.map((o) => {
            const r = ruleSummary(o);
            const high = o.stake === "high";
            return (
              <SettingsRow
                key={o.id}
                label={
                  <span data-testid="learning-rule" data-state={o.state} data-auto={o.auto ? "on" : "off"} className="flex flex-wrap items-center gap-2">
                    <span>{t(($) => $.learning.rule_label, { family: t(($) => $.learning.families[r.family as "gate" | "preview" | "watchdog" | "plan" | "pipeline_gate" | "second_approval" | "interview" | "question"] ?? r.family), label: r.option_label })}</span>
                    <span className={cn("rounded px-1 text-caption", o.state === "proposed" ? "bg-warning/15 text-warning" : "bg-muted text-muted-foreground")}>
                      {t(($) => $.learning.states[o.state])}
                    </span>
                    {high && <span className="rounded bg-destructive/10 px-1 text-caption text-destructive">{t(($) => $.learning.high_stakes)}</span>}
                  </span>
                }
                description={t(($) => $.learning.rule_description, { count: r.count, total: r.total, corrections: o.corrections, when: timeAgo(o.last_observed_at) })}
              >
                <span className="flex items-center gap-2">
                  <Switch
                    aria-label={t(($) => $.learning.auto_label)}
                    checked={o.auto}
                    disabled={high || setAuto.isPending}
                    onCheckedChange={(on) => setAuto.mutate({ id: o.id, auto: on }, { onError: (e) => fail(e, t(($) => $.learning.update_failed)) })}
                  />
                  <Button type="button" size="sm" variant="ghost" disabled={forget.isPending} onClick={() => forget.mutate(o.id, { onSuccess: () => toast.success(t(($) => $.learning.forgotten)), onError: (e) => fail(e, t(($) => $.learning.update_failed)) })}>
                    {t(($) => $.learning.forget)}
                  </Button>
                </span>
              </SettingsRow>
            );
          })}
        </SettingsCard>
      </SettingsSection>

      {hours && (
        <SettingsSection title={t(($) => $.learning.hours)} description={t(($) => $.learning.hours_description)}>
          <SettingsCard>
            <SettingsRow label={t(($) => $.learning.hours_label, { hours: busiestHours(hours).map((h) => `${h}h`).join(", ") || "–" })} description={t(($) => $.learning.hours_source, { count: hours.count, when: timeAgo(hours.last_observed_at) })}>
              <Button type="button" size="sm" variant="ghost" disabled={forget.isPending} onClick={() => forget.mutate(hours.id, { onSuccess: () => toast.success(t(($) => $.learning.forgotten)), onError: (e) => fail(e, t(($) => $.learning.update_failed)) })}>
                {t(($) => $.learning.forget)}
              </Button>
            </SettingsRow>
          </SettingsCard>
        </SettingsSection>
      )}
    </div>
  );
}
