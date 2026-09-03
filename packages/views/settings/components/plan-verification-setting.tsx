"use client";

import { useState } from "react";
import { ClipboardCheck } from "lucide-react";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { Workspace } from "@multica/core/types";
import { Switch } from "@multica/ui/components/ui/switch";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

export const PLAN_VERIFICATION_SETTING_KEY = "plan_verification_gate";

export function planVerificationGateEnabled(workspace: Workspace | null | undefined): boolean {
  const settings = (workspace?.settings as Record<string, unknown> | null | undefined) ?? {};
  return settings[PLAN_VERIFICATION_SETTING_KEY] === true;
}

/**
 * Workspace toggle for plan verification (F17): queues a verification run
 * after each completed run on an issue with a plan, and keeps an issue with
 * a critical finding out of done. Stored in workspace.settings like the
 * GitHub feature flags.
 */
export function PlanVerificationSetting({ workspace, canEdit }: { workspace: Workspace; canEdit: boolean }) {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const [saving, setSaving] = useState(false);
  const enabled = planVerificationGateEnabled(workspace);

  async function persist(next: boolean) {
    if (saving) return;
    setSaving(true);
    try {
      const merged = { ...((workspace.settings as Record<string, unknown>) ?? {}), [PLAN_VERIFICATION_SETTING_KEY]: next };
      const updated = await api.updateWorkspace(workspace.id, { settings: merged });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
      toast.success(t(($) => $.auto_save.toast_saved), { id: "settings-auto-save" });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.workspace.plan_verification_failed));
    } finally {
      setSaving(false);
    }
  }

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <ClipboardCheck className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.plan_verification_section)}
        </span>
      }
    >
      <SettingsCard>
        <SettingsRow
          label={t(($) => $.workspace.plan_verification_label)}
          description={t(($) => $.workspace.plan_verification_description)}
        >
          <Switch
            id="plan-verification-gate"
            aria-label={t(($) => $.workspace.plan_verification_label)}
            checked={enabled}
            disabled={!canEdit || saving}
            onCheckedChange={(v) => void persist(v === true)}
          />
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}
