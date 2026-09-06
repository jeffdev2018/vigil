"use client";

import { useEffect, useState } from "react";
import { ScrollText } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions } from "@multica/core/workspace/queries";
import {
  prWalkthroughSettingsOptions,
  useSavePrWalkthroughSettings,
  PR_WALKTHROUGH_DEFAULT_SETTINGS,
  type PrWalkthroughSettings,
} from "@multica/core/pr-walkthrough";
import { Switch } from "@multica/ui/components/ui/switch";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

const SELECT_CLASS =
  "h-8 w-56 rounded-md border border-input bg-background px-2 text-caption text-foreground disabled:cursor-not-allowed disabled:opacity-50";

/**
 * Narrative PR walkthrough (F05): the switch and the agent that writes the
 * walkthrough. Off by default — it spends a run on every push to every linked
 * pull request, so it has to be something someone chose.
 */
export function PrWalkthroughSetting({ canEdit }: { canEdit: boolean }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data: settings } = useQuery(prWalkthroughSettingsOptions(wsId));
  const { data: agents } = useQuery(agentListOptions(wsId));
  const save = useSavePrWalkthroughSettings(wsId);
  const [draft, setDraft] = useState<PrWalkthroughSettings>(PR_WALKTHROUGH_DEFAULT_SETTINGS);
  useEffect(() => {
    if (settings) setDraft(settings);
  }, [settings]);

  const persist = (next: PrWalkthroughSettings) => {
    setDraft(next);
    save.mutate(next, {
      onError: (e) => {
        // The server refuses to enable without an agent; show why and put the
        // switch back rather than leaving a lie on screen.
        setDraft(settings ?? PR_WALKTHROUGH_DEFAULT_SETTINGS);
        toast.error(e instanceof Error && e.message ? e.message : t(($) => $.workspace.pr_walkthrough_failed));
      },
    });
  };

  const disabled = !canEdit || save.isPending;
  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <ScrollText className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.pr_walkthrough_section)}
        </span>
      }
    >
      <SettingsCard>
        <div data-testid="pr-walkthrough-setting">
          <SettingsRow
            label={t(($) => $.workspace.pr_walkthrough_enabled)}
            description={t(($) => $.workspace.pr_walkthrough_intro)}
          >
            <Switch
              aria-label={t(($) => $.workspace.pr_walkthrough_enabled)}
              checked={draft.enabled}
              disabled={disabled || (!draft.enabled && !draft.agent_id)}
              onCheckedChange={(v) => persist({ ...draft, enabled: v === true })}
            />
          </SettingsRow>
          <SettingsRow
            label={t(($) => $.workspace.pr_walkthrough_agent)}
            description={t(($) => $.workspace.pr_walkthrough_agent_description)}
          >
            <select
              className={SELECT_CLASS}
              aria-label={t(($) => $.workspace.pr_walkthrough_agent)}
              value={draft.agent_id}
              disabled={disabled}
              onChange={(e) => persist({ ...draft, agent_id: e.target.value })}
            >
              <option value="">{t(($) => $.workspace.pr_walkthrough_pick_agent)}</option>
              {(agents ?? []).map((agent) => (
                <option key={agent.id} value={agent.id}>
                  {agent.name}
                </option>
              ))}
            </select>
          </SettingsRow>
        </div>
      </SettingsCard>
    </SettingsSection>
  );
}
