"use client";

import { useState } from "react";
import { BookMarked } from "lucide-react";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { Workspace } from "@multica/core/types";
import { Input } from "@multica/ui/components/ui/input";
import { Switch } from "@multica/ui/components/ui/switch";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

export const ADR_GATE_SETTING_KEY = "adr_gate";

export interface AdrGatePolicy {
  file_threshold: number;
  require_on_migration: boolean;
}

/** Mirrors the server defaults: absent means 10 files or a migration. */
export const ADR_GATE_DEFAULT: AdrGatePolicy = { file_threshold: 10, require_on_migration: true };

export function adrGatePolicy(workspace: Workspace | null | undefined): AdrGatePolicy {
  const settings = (workspace?.settings as Record<string, unknown> | null | undefined) ?? {};
  const raw = settings[ADR_GATE_SETTING_KEY];
  if (!raw || typeof raw !== "object") return ADR_GATE_DEFAULT;
  const r = raw as Record<string, unknown>;
  return {
    file_threshold: typeof r.file_threshold === "number" && r.file_threshold > 0 ? Math.floor(r.file_threshold) : 0,
    require_on_migration: r.require_on_migration === true,
  };
}

/**
 * Decision memory (K29): when a run is complex enough that its PR needs a
 * recorded decision before merge readiness clears. Both rules off turns the
 * gate off.
 */
export function AdrGateSetting({ workspace, canEdit }: { workspace: Workspace; canEdit: boolean }) {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const policy = adrGatePolicy(workspace);
  const [threshold, setThreshold] = useState(String(policy.file_threshold));
  const [saving, setSaving] = useState(false);

  async function persist(next: AdrGatePolicy) {
    if (saving) return;
    setSaving(true);
    try {
      const merged: Record<string, unknown> = { ...((workspace.settings as Record<string, unknown>) ?? {}), [ADR_GATE_SETTING_KEY]: next };
      const updated = await api.updateWorkspace(workspace.id, { settings: merged });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) => old?.map((ws) => (ws.id === updated.id ? updated : ws)));
      toast.success(t(($) => $.auto_save.toast_saved), { id: "settings-auto-save" });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.workspace.adr_gate_failed));
    } finally {
      setSaving(false);
    }
  }

  const commitThreshold = () => {
    const n = Math.max(0, Math.floor(Number(threshold) || 0));
    setThreshold(String(n));
    if (n !== policy.file_threshold) void persist({ ...policy, file_threshold: n });
  };

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <BookMarked className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.adr_gate_section)}
        </span>
      }
    >
      <SettingsCard>
        <SettingsRow label={t(($) => $.workspace.adr_gate_threshold_label)} description={t(($) => $.workspace.adr_gate_threshold_description)}>
          <Input
            type="number"
            min={0}
            aria-label={t(($) => $.workspace.adr_gate_threshold_label)}
            className="w-24"
            value={threshold}
            disabled={!canEdit || saving}
            onChange={(e) => setThreshold(e.target.value)}
            onBlur={commitThreshold}
            onKeyDown={(e) => {
              if (e.key === "Enter") commitThreshold();
            }}
          />
        </SettingsRow>
        <SettingsRow label={t(($) => $.workspace.adr_gate_migration_label)} description={t(($) => $.workspace.adr_gate_migration_description)}>
          <Switch
            aria-label={t(($) => $.workspace.adr_gate_migration_label)}
            checked={policy.require_on_migration}
            disabled={!canEdit || saving}
            onCheckedChange={(checked) => void persist({ ...policy, require_on_migration: checked === true })}
          />
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}
