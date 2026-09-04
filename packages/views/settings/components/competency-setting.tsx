"use client";

import { useEffect, useState } from "react";
import { GraduationCap } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { competencySettingsOptions, useSaveCompetencySettings } from "@multica/core/agents/competency";
import { Input } from "@multica/ui/components/ui/input";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

/** Learned competency (K43): the sample below which a score is flagged unreliable. */
export function CompetencySetting({ canEdit }: { canEdit: boolean }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data: settings } = useQuery(competencySettingsOptions(wsId));
  const save = useSaveCompetencySettings(wsId);
  const [draft, setDraft] = useState("5");
  useEffect(() => {
    if (settings) setDraft(String(settings.min_sample));
  }, [settings]);
  const commit = () => {
    const n = Math.max(1, Math.min(1000, Math.floor(Number(draft) || 0)));
    setDraft(String(n));
    if (settings && n === settings.min_sample) return;
    save.mutate({ min_sample: n }, { onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.workspace.competency_failed)) });
  };
  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <GraduationCap className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.competency_section)}
        </span>
      }
    >
      <SettingsCard>
        <SettingsRow label={t(($) => $.workspace.competency_min_sample)} description={t(($) => $.workspace.competency_min_sample_description)}>
          <Input type="number" min={1} max={1000} aria-label={t(($) => $.workspace.competency_min_sample)} className="w-24" value={draft} disabled={!canEdit || save.isPending} onChange={(e) => setDraft(e.target.value)} onBlur={commit} />
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}
