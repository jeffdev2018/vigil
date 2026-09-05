"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { TRUST_MODES, agentEffectModeOptions, agentTrustHistoryOptions, agentTrustModeOptions, agentTrustSuggestionOptions, pct, useSetAgentEffectMode, useSetAgentTrustMode, type TrustMode } from "@multica/core/agents/trust";
import type { Agent } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { useT, useTimeAgo } from "../../../i18n";

/**
 * Trust Dial (K26): the agent's autonomy mode, what each mode lets it do,
 * the promotion its scorecard earns (with the numbers that justify it) and
 * the log of every change, a demotion standing out. Nothing here changes a
 * mode without a click.
 */
export function TrustTab({ agent, canEdit }: { agent: Agent; canEdit: boolean }) {
  const { t } = useT("agents");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const { data: modeData } = useQuery(agentTrustModeOptions(wsId, agent.id));
  const { data: suggestion } = useQuery(agentTrustSuggestionOptions(wsId, agent.id));
  const { data: history = [] } = useQuery(agentTrustHistoryOptions(wsId, agent.id));
  const setMode = useSetAgentTrustMode(wsId, agent.id);
  const { data: effectMode } = useQuery(agentEffectModeOptions(wsId, agent.id));
  const setEffectMode = useSetAgentEffectMode(wsId, agent.id);
  const preview = (effectMode?.mode ?? agent.effect_mode ?? "apply") === "preview";
  const [pending, setPending] = useState<TrustMode | null>(null);
  const [reason, setReason] = useState("");
  const current = modeData?.mode ?? agent.trust_mode ?? "propose";

  const label = (m: string) => t(($) => $.trust.modes[m as TrustMode] ?? $.trust.modes.propose);
  const describe = (m: string) => t(($) => $.trust.describe[m as TrustMode] ?? $.trust.describe.propose);
  const apply = (mode: TrustMode) =>
    setMode.mutate(
      { mode, reason: reason.trim() || undefined },
      {
        onSuccess: () => {
          setPending(null);
          setReason("");
          toast.success(t(($) => $.trust.changed, { mode: label(mode) }));
        },
        onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.trust.change_failed)),
      },
    );

  return (
    <div data-testid="trust-tab" className="flex flex-col gap-4 text-caption">
      <p className="text-muted-foreground">{t(($) => $.trust.intro)}</p>

      {suggestion?.eligible && suggestion.suggested_mode && suggestion.suggested_mode !== current && (
        <div data-testid="trust-suggestion" className="flex flex-col gap-1 rounded-md border border-warning/50 p-3">
          <span className="font-medium">{t(($) => $.trust.suggestion_title, { mode: label(suggestion.suggested_mode) })}</span>
          <span className="text-muted-foreground">
            {t(($) => $.trust.suggestion_metrics, {
              runs: suggestion.metrics.runs_total, days: suggestion.metrics.days, accepted: pct(suggestion.metrics.accepted_rate),
              alone: pct(suggestion.metrics.no_intervention_rate), reopened: pct(suggestion.metrics.reopen_rate),
            })}
          </span>
          {canEdit && (
            <Button type="button" size="sm" className="self-start" disabled={setMode.isPending} onClick={() => setPending(suggestion.suggested_mode as TrustMode)}>
              {t(($) => $.trust.promote, { mode: label(suggestion.suggested_mode) })}
            </Button>
          )}
        </div>
      )}

      <div role="radiogroup" aria-label={t(($) => $.trust.mode_label)} className="flex flex-col gap-1.5">
        {TRUST_MODES.map((m) => {
          const active = m === current;
          return (
            <button
              key={m}
              type="button"
              role="radio"
              aria-checked={active}
              data-mode={m}
              disabled={!canEdit || setMode.isPending}
              onClick={() => !active && setPending(m)}
              className={cn("flex flex-col items-start gap-0.5 rounded-md border p-2 text-left transition-colors", active ? "border-primary bg-accent" : "hover:bg-accent/50", !canEdit && "cursor-default")}
            >
              <span className={cn("font-medium", active && "text-foreground")}>
                {label(m)}
                {active && <span className="ml-2 text-muted-foreground">· {t(($) => $.trust.current)}</span>}
              </span>
              <span className="text-muted-foreground">{describe(m)}</span>
            </button>
          );
        })}
      </div>

      {pending && (
        <div data-testid="trust-confirm" className="flex flex-col gap-2 rounded-md border p-3">
          <span className="font-medium">{t(($) => $.trust.confirm_title, { from: label(current), to: label(pending) })}</span>
          <Textarea aria-label={t(($) => $.trust.reason)} placeholder={t(($) => $.trust.reason_placeholder)} rows={2} value={reason} onChange={(e) => setReason(e.target.value)} />
          <div className="flex gap-2">
            <Button type="button" size="sm" disabled={setMode.isPending} onClick={() => apply(pending)}>{t(($) => $.trust.confirm)}</Button>
            <Button type="button" size="sm" variant="ghost" onClick={() => setPending(null)}>{t(($) => $.trust.cancel)}</Button>
          </div>
        </div>
      )}

      {/* "Show me first" (K69): the run's writes wait for one approval at the end. */}
      <div data-testid="effect-mode" data-mode={preview ? "preview" : "apply"} className="flex items-center gap-3 rounded-md border p-3">
        <div className="flex flex-1 flex-col gap-0.5">
          <span className="font-medium">{t(($) => $.trust.preview_title)}</span>
          <span className="text-muted-foreground">{t(($) => $.trust.preview_description)}</span>
        </div>
        <Switch
          aria-label={t(($) => $.trust.preview_title)}
          checked={preview}
          disabled={!canEdit || setEffectMode.isPending}
          onCheckedChange={(on) =>
            setEffectMode.mutate(on ? "preview" : "apply", {
              onSuccess: () => toast.success(t(($) => (on ? $.trust.preview_on : $.trust.preview_off))),
              onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.trust.preview_failed)),
            })
          }
        />
      </div>

      {suggestion && !suggestion.eligible && suggestion.reasons.length > 0 && current !== "autonomous" && (
        <p data-testid="trust-not-yet" className="text-muted-foreground">{t(($) => $.trust.not_yet, { reasons: suggestion.reasons.join(" · ") })}</p>
      )}

      <div>
        <div className="mb-1 font-medium">{t(($) => $.trust.history)}</div>
        {history.length === 0 ? (
          <p className="text-muted-foreground">{t(($) => $.trust.history_empty)}</p>
        ) : (
          <ul className="flex flex-col gap-1">
            {history.map((c) => (
              <li key={c.id} data-testid="trust-change" data-demotion={c.demotion ? "true" : "false"} className={cn("flex flex-wrap items-center gap-2 rounded-md border px-2 py-1", c.demotion && "border-destructive/50")}>
                <span className={cn("font-medium", c.demotion && "text-destructive")}>{label(c.from_mode)} → {label(c.to_mode)}</span>
                {c.demotion && <span className="rounded bg-destructive/10 px-1 text-destructive">{t(($) => $.trust.demotion)}</span>}
                <span className="text-muted-foreground">{c.triggered_by_type === "member" ? t(($) => $.trust.by_member) : t(($) => $.trust.by_system)} · {timeAgo(c.created_at)}</span>
                {c.reason && <span className="w-full text-muted-foreground">{c.reason}</span>}
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
