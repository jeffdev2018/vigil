"use client";

import { useEffect, useState } from "react";
import { ShieldCheck } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { crossReviewSettingsOptions, useSaveCrossReviewSettings, type CrossReviewSettings } from "@multica/core/issues/cross-review";
import { projectListOptions } from "@multica/core/projects/queries";
import { Switch } from "@multica/ui/components/ui/switch";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

/** Cross-provider review (K15): on/off for the workspace, and the projects that opt out. */
export function CrossReviewSetting({ canEdit }: { canEdit: boolean }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data: settings } = useQuery(crossReviewSettingsOptions(wsId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const save = useSaveCrossReviewSettings(wsId);
  const [draft, setDraft] = useState<CrossReviewSettings>({ enabled: true, opt_out_project_ids: [] });
  useEffect(() => {
    if (settings) setDraft(settings);
  }, [settings]);
  const persist = (next: CrossReviewSettings) => {
    setDraft(next);
    save.mutate(next, { onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.workspace.cross_review_failed)) });
  };
  const disabled = !canEdit || save.isPending;
  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <ShieldCheck className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.cross_review_section)}
        </span>
      }
    >
      <SettingsCard>
        <SettingsRow label={t(($) => $.workspace.cross_review_enabled)} description={t(($) => $.workspace.cross_review_enabled_description)}>
          <Switch aria-label={t(($) => $.workspace.cross_review_enabled)} checked={draft.enabled} disabled={disabled} onCheckedChange={(v) => persist({ ...draft, enabled: v === true })} />
        </SettingsRow>
        {projects.length > 0 && (
          <SettingsRow label={t(($) => $.workspace.cross_review_opt_out)} description={t(($) => $.workspace.cross_review_opt_out_description)}>
            <div data-testid="cross-review-opt-out" className="flex flex-col gap-1">
              {projects.map((p) => {
                const out = draft.opt_out_project_ids.includes(p.id);
                return (
                  <label key={p.id} className="flex items-center gap-2 text-caption">
                    <input type="checkbox" aria-label={p.title} checked={out} disabled={disabled || !draft.enabled} onChange={(e) => persist({ ...draft, opt_out_project_ids: e.target.checked ? [...draft.opt_out_project_ids, p.id] : draft.opt_out_project_ids.filter((id) => id !== p.id) })} />
                    <span>{p.title}</span>
                  </label>
                );
              })}
            </div>
          </SettingsRow>
        )}
      </SettingsCard>
    </SettingsSection>
  );
}
