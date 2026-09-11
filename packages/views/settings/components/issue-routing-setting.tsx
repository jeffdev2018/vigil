"use client";

import { useEffect, useState } from "react";
import { Route } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { RISK_LEVELS, routingSettingsOptions, useSaveRoutingSettings, type RiskLevel, type RoutingSettings } from "@multica/core/issues/routing";
import { runtimePoolsOptions } from "@multica/core/runtimes/pools";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@multica/ui/components/ui/select";
import { Switch } from "@multica/ui/components/ui/switch";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

/**
 * Issue router (K27): which pool each risk level runs on, and how many
 * consecutive failures push an issue one level up. Risk comes from the
 * project's blast radius rules; nothing here is inferred by a model.
 */
export function IssueRoutingSetting({ canEdit }: { canEdit: boolean }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data: settings } = useQuery(routingSettingsOptions(wsId));
  const { data: pools = [] } = useQuery(runtimePoolsOptions(wsId));
  const save = useSaveRoutingSettings(wsId);
  const [draft, setDraft] = useState<RoutingSettings>({ enabled: false, pools: {}, escalation_failures: 2 });
  useEffect(() => {
    if (settings) setDraft(settings);
  }, [settings]);
  const persist = (next: RoutingSettings) => {
    setDraft(next);
    save.mutate(next, { onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.workspace.routing_failed)) });
  };
  const disabled = !canEdit || save.isPending;

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <Route className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.routing_section)}
        </span>
      }
    >
      <SettingsCard>
        <div data-testid="issue-routing-setting">
          <SettingsRow label={t(($) => $.workspace.routing_enabled)} description={t(($) => $.workspace.routing_intro)}>
            <Switch aria-label={t(($) => $.workspace.routing_enabled)} checked={draft.enabled} disabled={disabled} onCheckedChange={(v) => persist({ ...draft, enabled: v })} />
          </SettingsRow>
          {RISK_LEVELS.map((level: RiskLevel) => (
            <SettingsRow key={level} label={t(($) => $.workspace.routing_levels[level])} description={t(($) => $.workspace.routing_level_hints[level])}>
              <Select
                items={[
                  { value: "", label: t(($) => $.workspace.routing_pool_agent) },
                  ...pools.map((p) => ({ value: p.id, label: p.name })),
                ]}
                value={draft.pools[level] ?? ""}
                onValueChange={(value) => {
                  const nextPools = { ...draft.pools };
                  if (value) nextPools[level] = value;
                  else delete nextPools[level];
                  persist({ ...draft, pools: nextPools });
                }}
              >
                <SelectTrigger
                  aria-label={t(($) => $.workspace.routing_pool_aria, { level: t(($) => $.workspace.routing_levels[level]) })}
                  size="sm"
                  disabled={disabled}
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="">{t(($) => $.workspace.routing_pool_agent)}</SelectItem>
                  {pools.map((p) => <SelectItem key={p.id} value={p.id}>{p.name}</SelectItem>)}
                </SelectContent>
              </Select>
            </SettingsRow>
          ))}
          <SettingsRow label={t(($) => $.workspace.routing_escalation)} description={t(($) => $.workspace.routing_escalation_description)}>
            <Input
              type="number"
              min={0}
              max={20}
              aria-label={t(($) => $.workspace.routing_escalation)}
              className="w-24"
              value={draft.escalation_failures}
              disabled={disabled}
              onChange={(e) => setDraft({ ...draft, escalation_failures: Math.max(0, Math.min(20, Math.floor(Number(e.target.value) || 0))) })}
              onBlur={() => {
                if (settings && draft.escalation_failures !== settings.escalation_failures) persist(draft);
              }}
            />
          </SettingsRow>
        </div>
      </SettingsCard>
    </SettingsSection>
  );
}
