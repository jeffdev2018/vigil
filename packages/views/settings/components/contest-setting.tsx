"use client";

import { useEffect, useState } from "react";
import { Swords } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { CONTEST_TARGET_TYPES, contestSettingsOptions, useSaveContestSettings, type ContestSettings } from "@multica/core/issues/contest";
import { projectListOptions } from "@multica/core/projects/queries";
import { Switch } from "@multica/ui/components/ui/switch";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

const DEFAULTS: ContestSettings = { targets: Object.fromEntries(CONTEST_TARGET_TYPES.map((k) => [k, true])), opt_out_project_ids: [] };

/** Contest (K72): which outputs may be challenged, and the projects that opt out. */
export function ContestSetting({ canEdit }: { canEdit: boolean }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data: settings } = useQuery(contestSettingsOptions(wsId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const save = useSaveContestSettings(wsId);
  const [draft, setDraft] = useState<ContestSettings>(DEFAULTS);
  useEffect(() => {
    if (settings) setDraft(settings);
  }, [settings]);
  const persist = (next: ContestSettings) => {
    setDraft(next);
    save.mutate(next, { onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.workspace.contest_failed)) });
  };
  const disabled = !canEdit || save.isPending;
  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <Swords className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.contest_section)}
        </span>
      }
    >
      <SettingsCard>
        {CONTEST_TARGET_TYPES.map((target) => (
          <SettingsRow key={target} label={t(($) => $.workspace[`contest_target_${target}`])} description={target === "task_result" ? t(($) => $.workspace.contest_description) : undefined}>
            <Switch aria-label={t(($) => $.workspace[`contest_target_${target}`])} checked={draft.targets[target] === true} disabled={disabled} onCheckedChange={(v) => persist({ ...draft, targets: { ...draft.targets, [target]: v === true } })} />
          </SettingsRow>
        ))}
        {projects.length > 0 && (
          <SettingsRow label={t(($) => $.workspace.contest_opt_out)} description={t(($) => $.workspace.contest_opt_out_description)}>
            <div data-testid="contest-opt-out" className="flex flex-col gap-1">
              {projects.map((p) => {
                const out = draft.opt_out_project_ids.includes(p.id);
                return (
                  <label key={p.id} className="flex items-center gap-2 text-caption">
                    <input type="checkbox" aria-label={p.title} checked={out} disabled={disabled} onChange={(e) => persist({ ...draft, opt_out_project_ids: e.target.checked ? [...draft.opt_out_project_ids, p.id] : draft.opt_out_project_ids.filter((id) => id !== p.id) })} />
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
