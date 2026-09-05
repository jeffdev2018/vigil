"use client";

import { useEffect, useState } from "react";
import { Wrench } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { ciAutoFixSettingsOptions, useSaveCIAutoFixSettings, type CIAutoFixSettings } from "@multica/core/issues/ci-auto-fix";
import { Input } from "@multica/ui/components/ui/input";
import { Switch } from "@multica/ui/components/ui/switch";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

const TICKS_PER_USD = 1e10;

/** CI auto-fix (K49): the switch, the attempts cap per pull request and the budget of one correction run. */
export function CIAutoFixSetting({ canEdit }: { canEdit: boolean }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data: settings } = useQuery(ciAutoFixSettingsOptions(wsId));
  const save = useSaveCIAutoFixSettings(wsId);
  const [draft, setDraft] = useState<CIAutoFixSettings>({ enabled: false, max_attempts: 3, budget_usd_ticks: 0 });
  const [budget, setBudget] = useState("0");
  useEffect(() => {
    if (settings) {
      setDraft(settings);
      setBudget(String(settings.budget_usd_ticks / TICKS_PER_USD));
    }
  }, [settings]);
  const persist = (next: CIAutoFixSettings) => {
    setDraft(next);
    save.mutate(next, { onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.workspace.ci_auto_fix_failed)) });
  };
  const disabled = !canEdit || save.isPending;
  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <Wrench className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.ci_auto_fix_section)}
        </span>
      }
    >
      <SettingsCard>
        <SettingsRow label={t(($) => $.workspace.ci_auto_fix_enabled)} description={t(($) => $.workspace.ci_auto_fix_enabled_description)}>
          <Switch aria-label={t(($) => $.workspace.ci_auto_fix_enabled)} checked={draft.enabled} disabled={disabled} onCheckedChange={(v) => persist({ ...draft, enabled: v === true })} />
        </SettingsRow>
        <SettingsRow label={t(($) => $.workspace.ci_auto_fix_attempts)} description={t(($) => $.workspace.ci_auto_fix_attempts_description)}>
          <Input type="number" min={1} max={20} aria-label={t(($) => $.workspace.ci_auto_fix_attempts)} className="w-24" value={draft.max_attempts} disabled={disabled} onChange={(e) => setDraft({ ...draft, max_attempts: Math.max(1, Math.min(20, Math.floor(Number(e.target.value) || 1))) })} onBlur={() => { if (settings && draft.max_attempts !== settings.max_attempts) persist(draft); }} />
        </SettingsRow>
        <SettingsRow label={t(($) => $.workspace.ci_auto_fix_budget)} description={t(($) => $.workspace.ci_auto_fix_budget_description)}>
          <Input type="number" min={0} step={0.1} aria-label={t(($) => $.workspace.ci_auto_fix_budget)} className="w-24" value={budget} disabled={disabled} onChange={(e) => setBudget(e.target.value)} onBlur={() => { const ticks = Math.max(0, Math.round((Number(budget) || 0) * TICKS_PER_USD)); if (settings && ticks !== settings.budget_usd_ticks) persist({ ...draft, budget_usd_ticks: ticks }); }} />
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}
