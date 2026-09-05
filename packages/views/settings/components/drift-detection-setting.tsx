"use client";

import { useEffect, useState } from "react";
import { Repeat } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { driftPolicyOptions, useSaveDriftPolicy, type DriftPolicy } from "@multica/core/issues/drift";
import { Input } from "@multica/ui/components/ui/input";
import { Switch } from "@multica/ui/components/ui/switch";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

/** Drift detection (K40): when a run going in circles is stopped. */
export function DriftDetectionSetting({ canEdit }: { canEdit: boolean }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data: policy } = useQuery(driftPolicyOptions(wsId));
  const save = useSaveDriftPolicy(wsId);
  const [draft, setDraft] = useState<DriftPolicy>({ enabled: true, repeated_action_threshold: 5, file_reread_threshold: 8 });
  useEffect(() => {
    if (policy) setDraft(policy);
  }, [policy]);
  const persist = (next: DriftPolicy) => {
    setDraft(next);
    save.mutate(next, { onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.workspace.drift_failed)) });
  };
  const clamp = (v: string) => Math.max(2, Math.min(100, Math.floor(Number(v) || 2)));
  const disabled = !canEdit || save.isPending;
  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <Repeat className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.drift_section)}
        </span>
      }
    >
      <SettingsCard>
        <div data-testid="drift-detection-setting">
          <SettingsRow label={t(($) => $.workspace.drift_enabled)} description={t(($) => $.workspace.drift_intro)}>
            <Switch aria-label={t(($) => $.workspace.drift_enabled)} checked={draft.enabled} disabled={disabled} onCheckedChange={(v) => persist({ ...draft, enabled: v })} />
          </SettingsRow>
          <SettingsRow label={t(($) => $.workspace.drift_repeated)} description={t(($) => $.workspace.drift_repeated_description)}>
            <Input type="number" min={2} max={100} className="w-24" aria-label={t(($) => $.workspace.drift_repeated)} value={draft.repeated_action_threshold} disabled={disabled} onChange={(e) => setDraft({ ...draft, repeated_action_threshold: clamp(e.target.value) })} onBlur={() => policy && draft.repeated_action_threshold !== policy.repeated_action_threshold && persist(draft)} />
          </SettingsRow>
          <SettingsRow label={t(($) => $.workspace.drift_reread)} description={t(($) => $.workspace.drift_reread_description)}>
            <Input type="number" min={2} max={100} className="w-24" aria-label={t(($) => $.workspace.drift_reread)} value={draft.file_reread_threshold} disabled={disabled} onChange={(e) => setDraft({ ...draft, file_reread_threshold: clamp(e.target.value) })} onBlur={() => policy && draft.file_reread_threshold !== policy.file_reread_threshold && persist(draft)} />
          </SettingsRow>
        </div>
      </SettingsCard>
    </SettingsSection>
  );
}
