"use client";

import { useState } from "react";
import { Users } from "lucide-react";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { Workspace } from "@multica/core/types";
import { Input } from "@multica/ui/components/ui/input";
import { Switch } from "@multica/ui/components/ui/switch";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

export const STANDUP_SETTING_KEY = "standup";

export interface StandupPolicy {
  enabled: boolean;
  blocked_hours: number;
  weekly_retro: boolean;
}

export function standupPolicy(workspace: Workspace | null | undefined): StandupPolicy {
  const settings = (workspace?.settings as Record<string, unknown> | null | undefined) ?? {};
  const raw = (settings[STANDUP_SETTING_KEY] ?? {}) as Record<string, unknown>;
  const hours = typeof raw.blocked_hours === "number" && raw.blocked_hours > 0 ? Math.floor(raw.blocked_hours) : 24;
  return { enabled: raw.enabled === true, blocked_hours: hours, weekly_retro: raw.weekly_retro === true };
}

/**
 * Standup and retro (K34): whether the standup asks about issues stuck
 * longer than the threshold, and whether the weekly retro is generated and
 * sent to the leads. The local clock is the morning briefing timezone.
 */
export function StandupSetting({ workspace, canEdit }: { workspace: Workspace; canEdit: boolean }) {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const policy = standupPolicy(workspace);
  const [hours, setHours] = useState(String(policy.blocked_hours));
  const [saving, setSaving] = useState(false);

  async function persist(next: StandupPolicy) {
    if (saving) return;
    setSaving(true);
    try {
      const merged = { ...((workspace.settings as Record<string, unknown>) ?? {}), [STANDUP_SETTING_KEY]: next };
      const updated = await api.updateWorkspace(workspace.id, { settings: merged });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) => old?.map((ws) => (ws.id === updated.id ? updated : ws)));
      toast.success(t(($) => $.auto_save.toast_saved), { id: "settings-auto-save" });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.workspace.standup_failed));
    } finally {
      setSaving(false);
    }
  }

  const commitHours = () => {
    const h = Math.max(1, Math.floor(Number(hours) || 24));
    setHours(String(h));
    if (h !== policy.blocked_hours) void persist({ ...policy, blocked_hours: h });
  };

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <Users className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.standup_section)}
        </span>
      }
    >
      <SettingsCard>
        <SettingsRow label={t(($) => $.workspace.standup_enabled_label)} description={t(($) => $.workspace.standup_enabled_description)}>
          <Switch aria-label={t(($) => $.workspace.standup_enabled_label)} checked={policy.enabled} disabled={!canEdit || saving} onCheckedChange={(v) => void persist({ ...policy, enabled: v === true })} />
        </SettingsRow>
        <SettingsRow label={t(($) => $.workspace.standup_hours_label)} description={t(($) => $.workspace.standup_hours_description)}>
          <Input type="number" min={1} aria-label={t(($) => $.workspace.standup_hours_label)} className="w-24" value={hours} disabled={!canEdit || saving} onChange={(e) => setHours(e.target.value)} onBlur={commitHours} onKeyDown={(e) => { if (e.key === "Enter") commitHours(); }} />
        </SettingsRow>
        <SettingsRow label={t(($) => $.workspace.standup_retro_label)} description={t(($) => $.workspace.standup_retro_description)}>
          <Switch aria-label={t(($) => $.workspace.standup_retro_label)} checked={policy.weekly_retro} disabled={!canEdit || saving} onCheckedChange={(v) => void persist({ ...policy, weekly_retro: v === true })} />
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}
