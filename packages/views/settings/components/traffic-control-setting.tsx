"use client";

import { useState } from "react";
import { GitMerge } from "lucide-react";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { Workspace } from "@multica/core/types";
import { Switch } from "@multica/ui/components/ui/switch";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

export const TRAFFIC_CONTROL_SETTING_KEY = "traffic_control";

export function trafficControlPauses(workspace: Workspace | null | undefined): boolean {
  const settings = (workspace?.settings as Record<string, unknown> | null | undefined) ?? {};
  const raw = (settings[TRAFFIC_CONTROL_SETTING_KEY] ?? {}) as Record<string, unknown>;
  return raw.pause_on_conflict === true;
}

/** Traffic control (K18): alert only, or pause the run when it collides. */
export function TrafficControlSetting({ workspace, canEdit }: { workspace: Workspace; canEdit: boolean }) {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const [saving, setSaving] = useState(false);
  const pauses = trafficControlPauses(workspace);
  async function persist(next: boolean) {
    if (saving) return;
    setSaving(true);
    try {
      const merged = { ...((workspace.settings as Record<string, unknown>) ?? {}), [TRAFFIC_CONTROL_SETTING_KEY]: { pause_on_conflict: next } };
      const updated = await api.updateWorkspace(workspace.id, { settings: merged });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) => old?.map((ws) => (ws.id === updated.id ? updated : ws)));
      toast.success(t(($) => $.auto_save.toast_saved), { id: "settings-auto-save" });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.workspace.traffic_failed));
    } finally {
      setSaving(false);
    }
  }
  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <GitMerge className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.traffic_section)}
        </span>
      }
    >
      <SettingsCard>
        <SettingsRow label={t(($) => $.workspace.traffic_pause_label)} description={t(($) => $.workspace.traffic_pause_description)}>
          <Switch aria-label={t(($) => $.workspace.traffic_pause_label)} checked={pauses} disabled={!canEdit || saving} onCheckedChange={(v) => void persist(v)} />
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}
