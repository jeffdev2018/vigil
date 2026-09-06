"use client";

import { useEffect, useState } from "react";
import { Workflow } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { workflowPolicySettingsOptions, useSaveWorkflowPolicySettings, type WorkflowPolicySettings } from "@multica/core/issues/workflow-policy";
import { Switch } from "@multica/ui/components/ui/switch";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

/** Workflow policy (JEF-273): whether the selector learns each task's workflow from run history or always runs single. */
export function WorkflowPolicySetting({ canEdit }: { canEdit: boolean }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data: settings } = useQuery(workflowPolicySettingsOptions(wsId));
  const save = useSaveWorkflowPolicySettings(wsId);
  const [draft, setDraft] = useState<WorkflowPolicySettings>({ mode: "off" });
  useEffect(() => {
    if (settings) setDraft(settings);
  }, [settings]);
  const persist = (next: WorkflowPolicySettings) => {
    setDraft(next);
    save.mutate(next, { onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.workspace.workflow_policy_failed)) });
  };
  const disabled = !canEdit || save.isPending;
  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <Workflow className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.workflow_policy_section)}
        </span>
      }
    >
      <SettingsCard>
        <SettingsRow label={t(($) => $.workspace.workflow_policy_auto)} description={t(($) => $.workspace.workflow_policy_auto_description)}>
          <Switch
            aria-label={t(($) => $.workspace.workflow_policy_auto)}
            checked={draft.mode === "auto"}
            disabled={disabled}
            onCheckedChange={(v) => persist({ mode: v === true ? "auto" : "off" })}
          />
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}
