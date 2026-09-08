"use client";

import { useEffect, useState } from "react";
import { Gauge } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { confidenceReviewSettingsOptions, useSaveConfidenceReviewSettings, type ConfidenceReviewSettings } from "@multica/core/issues/confidence-review";
import { Input } from "@multica/ui/components/ui/input";
import { Switch } from "@multica/ui/components/ui/switch";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

/** Confidence review (JEF-240): the switch, and the score under which a run is routed to human review. */
export function ConfidenceReviewSetting({ canEdit }: { canEdit: boolean }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data: settings } = useQuery(confidenceReviewSettingsOptions(wsId));
  const save = useSaveConfidenceReviewSettings(wsId);
  const [draft, setDraft] = useState<ConfidenceReviewSettings>({ enabled: true, threshold: 0.5, max_escalations: 2 });
  useEffect(() => {
    if (settings) setDraft(settings);
  }, [settings]);
  const persist = (next: ConfidenceReviewSettings) => {
    setDraft(next);
    save.mutate(next, { onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.workspace.confidence_review_failed)) });
  };
  const disabled = !canEdit || save.isPending;
  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <Gauge className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.confidence_review_section)}
        </span>
      }
    >
      <SettingsCard>
        <SettingsRow label={t(($) => $.workspace.confidence_review_enabled)} description={t(($) => $.workspace.confidence_review_enabled_description)}>
          <Switch aria-label={t(($) => $.workspace.confidence_review_enabled)} checked={draft.enabled} disabled={disabled} onCheckedChange={(v) => persist({ ...draft, enabled: v === true })} />
        </SettingsRow>
        <SettingsRow label={t(($) => $.workspace.confidence_review_threshold)} description={t(($) => $.workspace.confidence_review_threshold_description)}>
          <Input
            type="number"
            min={0.01}
            max={1}
            step={0.05}
            aria-label={t(($) => $.workspace.confidence_review_threshold)}
            className="w-24"
            value={draft.threshold}
            disabled={disabled || !draft.enabled}
            onChange={(e) => setDraft({ ...draft, threshold: Number(e.target.value) })}
            onBlur={() => {
              // Server validation is 0 < threshold ≤ 1; clamp to it here so
              // the mutation never ships a value the API would reject.
              const threshold = Math.min(1, Math.max(0.01, Math.round((Number(draft.threshold) || 0.5) * 100) / 100));
              if (threshold !== draft.threshold) setDraft({ ...draft, threshold });
              if (settings && threshold !== settings.threshold) persist({ ...draft, threshold });
            }}
          />
        </SettingsRow>
        <SettingsRow label={t(($) => $.workspace.confidence_review_max_escalations)} description={t(($) => $.workspace.confidence_review_max_escalations_description)}>
          <Input
            type="number"
            min={0}
            max={3}
            step={1}
            aria-label={t(($) => $.workspace.confidence_review_max_escalations)}
            className="w-24"
            value={draft.max_escalations}
            disabled={disabled || !draft.enabled}
            onChange={(e) => setDraft({ ...draft, max_escalations: Number(e.target.value) })}
            onBlur={() => {
              // Server contract is an integer in [0, 3]; clamp to it here so
              // the mutation never ships a value the API would reject.
              const maxEscalations = Math.min(3, Math.max(0, Math.round(Number(draft.max_escalations) || 0)));
              if (maxEscalations !== draft.max_escalations) setDraft({ ...draft, max_escalations: maxEscalations });
              if (settings && maxEscalations !== settings.max_escalations) persist({ ...draft, max_escalations: maxEscalations });
            }}
          />
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}
