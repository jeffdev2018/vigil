"use client";

import { useState } from "react";
import { Hourglass } from "lucide-react";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { memberListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import type { Workspace } from "@multica/core/types";
import { Input } from "@multica/ui/components/ui/input";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

export const DECISION_SLA_SETTING_KEY = "decision_sla";

export interface DecisionSlaPolicy {
  deadline_minutes: number;
  substitute_user_id: string;
}

/** The policy as stored, or null when off (no key or no positive deadline). */
export function decisionSlaPolicy(workspace: Workspace | null | undefined): DecisionSlaPolicy | null {
  const settings = (workspace?.settings as Record<string, unknown> | null | undefined) ?? {};
  const raw = settings[DECISION_SLA_SETTING_KEY];
  if (!raw || typeof raw !== "object") return null;
  const r = raw as Record<string, unknown>;
  const minutes = typeof r.deadline_minutes === "number" ? r.deadline_minutes : 0;
  if (!(minutes > 0)) return null;
  return { deadline_minutes: minutes, substitute_user_id: typeof r.substitute_user_id === "string" ? r.substitute_user_id : "" };
}

/**
 * Decision SLA (K35): how long a Decision Card may wait, and who hears about
 * it first when it passes (then the workspace leads). Stored in
 * workspace.settings like the plan verification gate; 0 minutes turns it off.
 */
export function DecisionSlaSetting({ workspace, canEdit }: { workspace: Workspace; canEdit: boolean }) {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const policy = decisionSlaPolicy(workspace);
  const [minutes, setMinutes] = useState(String(policy?.deadline_minutes ?? 0));
  const [saving, setSaving] = useState(false);
  const { data: members = [] } = useQuery(memberListOptions(workspace.id));

  async function persist(next: DecisionSlaPolicy | null) {
    if (saving) return;
    setSaving(true);
    try {
      const merged: Record<string, unknown> = { ...((workspace.settings as Record<string, unknown>) ?? {}) };
      if (next) merged[DECISION_SLA_SETTING_KEY] = next;
      else delete merged[DECISION_SLA_SETTING_KEY];
      const updated = await api.updateWorkspace(workspace.id, { settings: merged });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
      toast.success(t(($) => $.auto_save.toast_saved), { id: "settings-auto-save" });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.workspace.decision_sla_failed));
    } finally {
      setSaving(false);
    }
  }

  const commitMinutes = () => {
    const n = Math.max(0, Math.floor(Number(minutes) || 0));
    setMinutes(String(n));
    if (n === (policy?.deadline_minutes ?? 0)) return;
    void persist(n > 0 ? { deadline_minutes: n, substitute_user_id: policy?.substitute_user_id ?? "" } : null);
  };

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <Hourglass className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.decision_sla_section)}
        </span>
      }
    >
      <SettingsCard>
        <SettingsRow label={t(($) => $.workspace.decision_sla_deadline_label)} description={t(($) => $.workspace.decision_sla_deadline_description)}>
          <Input
            type="number"
            min={0}
            aria-label={t(($) => $.workspace.decision_sla_deadline_label)}
            className="w-24"
            value={minutes}
            disabled={!canEdit || saving}
            onChange={(e) => setMinutes(e.target.value)}
            onBlur={commitMinutes}
            onKeyDown={(e) => {
              if (e.key === "Enter") commitMinutes();
            }}
          />
        </SettingsRow>
        <SettingsRow label={t(($) => $.workspace.decision_sla_substitute_label)} description={t(($) => $.workspace.decision_sla_substitute_description)}>
          <select
            aria-label={t(($) => $.workspace.decision_sla_substitute_label)}
            className="h-8 rounded-md border bg-background px-2 text-caption"
            value={policy?.substitute_user_id ?? ""}
            disabled={!canEdit || saving || !policy}
            onChange={(e) => policy && void persist({ ...policy, substitute_user_id: e.target.value })}
          >
            <option value="">{t(($) => $.workspace.decision_sla_substitute_none)}</option>
            {members.map((m) => (
              <option key={m.user_id} value={m.user_id}>
                {m.name || m.email}
              </option>
            ))}
          </select>
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}
