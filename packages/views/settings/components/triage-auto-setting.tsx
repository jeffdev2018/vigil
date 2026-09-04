"use client";

import { useState } from "react";
import { Sparkles } from "lucide-react";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { Workspace } from "@multica/core/types";
import { Input } from "@multica/ui/components/ui/input";
import { Switch } from "@multica/ui/components/ui/switch";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

export const TRIAGE_AUTO_SETTING_KEY = "triage_auto";

export interface TriageAutoPolicy {
  enabled: boolean;
  threshold: number;
  min_examples: number;
}

export function triageAutoPolicy(workspace: Workspace | null | undefined): TriageAutoPolicy {
  const settings = (workspace?.settings as Record<string, unknown> | null | undefined) ?? {};
  const raw = (settings[TRIAGE_AUTO_SETTING_KEY] ?? {}) as Record<string, unknown>;
  const threshold = typeof raw.threshold === "number" && raw.threshold > 0 && raw.threshold <= 1 ? raw.threshold : 0.9;
  const min = typeof raw.min_examples === "number" && raw.min_examples > 0 ? Math.floor(raw.min_examples) : 20;
  return { enabled: raw.enabled === true, threshold, min_examples: min };
}

/**
 * Triage auto-ML (K61): whether the queue may apply a confident suggestion
 * by itself, the confidence it needs and how many past decisions it must
 * have learned from first. Off by default.
 */
export function TriageAutoSetting({ workspace, canEdit }: { workspace: Workspace; canEdit: boolean }) {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const policy = triageAutoPolicy(workspace);
  const [threshold, setThreshold] = useState(String(Math.round(policy.threshold * 100)));
  const [min, setMin] = useState(String(policy.min_examples));
  const [saving, setSaving] = useState(false);

  async function persist(next: TriageAutoPolicy) {
    if (saving) return;
    setSaving(true);
    try {
      const merged = { ...((workspace.settings as Record<string, unknown>) ?? {}), [TRIAGE_AUTO_SETTING_KEY]: next };
      const updated = await api.updateWorkspace(workspace.id, { settings: merged });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) => old?.map((ws) => (ws.id === updated.id ? updated : ws)));
      toast.success(t(($) => $.auto_save.toast_saved), { id: "settings-auto-save" });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.workspace.triage_auto_failed));
    } finally {
      setSaving(false);
    }
  }
  const commit = () => {
    const pct = Math.min(100, Math.max(50, Math.floor(Number(threshold) || 90)));
    const m = Math.max(1, Math.floor(Number(min) || 20));
    setThreshold(String(pct));
    setMin(String(m));
    if (pct / 100 !== policy.threshold || m !== policy.min_examples) void persist({ ...policy, threshold: pct / 100, min_examples: m });
  };

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <Sparkles className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.triage_auto_section)}
        </span>
      }
    >
      <SettingsCard>
        <SettingsRow label={t(($) => $.workspace.triage_auto_enabled_label)} description={t(($) => $.workspace.triage_auto_enabled_description)}>
          <Switch aria-label={t(($) => $.workspace.triage_auto_enabled_label)} checked={policy.enabled} disabled={!canEdit || saving} onCheckedChange={(v) => void persist({ ...policy, enabled: v === true })} />
        </SettingsRow>
        <SettingsRow label={t(($) => $.workspace.triage_auto_threshold_label)} description={t(($) => $.workspace.triage_auto_threshold_description)}>
          <Input type="number" min={50} max={100} aria-label={t(($) => $.workspace.triage_auto_threshold_label)} className="w-24" value={threshold} disabled={!canEdit || saving} onChange={(e) => setThreshold(e.target.value)} onBlur={commit} />
        </SettingsRow>
        <SettingsRow label={t(($) => $.workspace.triage_auto_min_label)} description={t(($) => $.workspace.triage_auto_min_description)}>
          <Input type="number" min={1} aria-label={t(($) => $.workspace.triage_auto_min_label)} className="w-24" value={min} disabled={!canEdit || saving} onChange={(e) => setMin(e.target.value)} onBlur={commit} />
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}
