"use client";

import { useState } from "react";
import { ShieldCheck } from "lucide-react";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { Workspace } from "@multica/core/types";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

export const APPROVAL_GATES_SETTING_KEY = "approval_gates";

// Mirrors server/internal/service/approval_gates.go's ApprovalGates.Approvers.
export const GATE_APPROVERS_ANY_MEMBER = "any_member";
export const GATE_APPROVERS_OWNER_ADMIN = "owner_admin";

export interface ApprovalGatesPolicy {
  timeout_minutes: number;
  spend_threshold_usd_ticks: number;
  sensitive_tools: string;
  approvers: typeof GATE_APPROVERS_ANY_MEMBER | typeof GATE_APPROVERS_OWNER_ADMIN;
}

export const DEFAULT_SENSITIVE_TOOLS = "(?i)merge|delete|remove|drop|destroy|pay|charge|transfer|refund|purchase";

export function approvalGatesPolicy(workspace: Workspace | null | undefined): ApprovalGatesPolicy {
  const settings = (workspace?.settings as Record<string, unknown> | null | undefined) ?? {};
  const raw = (settings[APPROVAL_GATES_SETTING_KEY] ?? {}) as Record<string, unknown>;
  return {
    timeout_minutes: typeof raw.timeout_minutes === "number" && raw.timeout_minutes > 0 ? Math.floor(raw.timeout_minutes) : 30,
    spend_threshold_usd_ticks: typeof raw.spend_threshold_usd_ticks === "number" && raw.spend_threshold_usd_ticks > 0 ? raw.spend_threshold_usd_ticks : 100_000_000_000,
    sensitive_tools: typeof raw.sensitive_tools === "string" && raw.sensitive_tools !== "" ? raw.sensitive_tools : DEFAULT_SENSITIVE_TOOLS,
    approvers: raw.approvers === GATE_APPROVERS_OWNER_ADMIN ? GATE_APPROVERS_OWNER_ADMIN : GATE_APPROVERS_ANY_MEMBER,
  };
}

/**
 * Approval gates (K05): how long a blocked action waits for a human, the
 * spend under which no approval is asked, and which MCP tools pause.
 */
export function ApprovalGatesSetting({ workspace, canEdit }: { workspace: Workspace; canEdit: boolean }) {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const policy = approvalGatesPolicy(workspace);
  const [timeout, setTimeout_] = useState(String(policy.timeout_minutes));
  const [spend, setSpend] = useState((policy.spend_threshold_usd_ticks / 1e10).toFixed(2));
  const [tools, setTools] = useState(policy.sensitive_tools);
  const [approvers, setApprovers] = useState<ApprovalGatesPolicy["approvers"]>(policy.approvers);
  const [saving, setSaving] = useState(false);

  async function persist(next: ApprovalGatesPolicy) {
    if (saving) return;
    setSaving(true);
    try {
      const merged = { ...((workspace.settings as Record<string, unknown>) ?? {}), [APPROVAL_GATES_SETTING_KEY]: next };
      const updated = await api.updateWorkspace(workspace.id, { settings: merged });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) => old?.map((ws) => (ws.id === updated.id ? updated : ws)));
      toast.success(t(($) => $.auto_save.toast_saved), { id: "settings-auto-save" });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.workspace.gates_failed));
    } finally {
      setSaving(false);
    }
  }
  const commit = () => {
    const minutes = Math.max(1, Math.floor(Number(timeout) || 30));
    const ticks = Math.max(0, Math.round((Number(spend) || 0) * 1e10));
    const pattern = tools.trim() || DEFAULT_SENSITIVE_TOOLS;
    setTimeout_(String(minutes));
    setSpend((ticks / 1e10).toFixed(2));
    setTools(pattern);
    if (minutes !== policy.timeout_minutes || ticks !== policy.spend_threshold_usd_ticks || pattern !== policy.sensitive_tools) {
      void persist({ timeout_minutes: minutes, spend_threshold_usd_ticks: ticks, sensitive_tools: pattern, approvers });
    }
  };
  const commitApprovers = (next: ApprovalGatesPolicy["approvers"]) => {
    if (!next || next === approvers) return;
    setApprovers(next);
    void persist({ ...policy, approvers: next });
  };

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <ShieldCheck className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.gates_section)}
        </span>
      }
    >
      <SettingsCard>
        <SettingsRow label={t(($) => $.workspace.gates_timeout_label)} description={t(($) => $.workspace.gates_timeout_description)}>
          <Input type="number" min={1} aria-label={t(($) => $.workspace.gates_timeout_label)} className="w-24" value={timeout} disabled={!canEdit || saving} onChange={(e) => setTimeout_(e.target.value)} onBlur={commit} />
        </SettingsRow>
        <SettingsRow label={t(($) => $.workspace.gates_spend_label)} description={t(($) => $.workspace.gates_spend_description)}>
          <Input type="number" min={0} step={0.5} aria-label={t(($) => $.workspace.gates_spend_label)} className="w-28" value={spend} disabled={!canEdit || saving} onChange={(e) => setSpend(e.target.value)} onBlur={commit} />
        </SettingsRow>
        <SettingsRow label={t(($) => $.workspace.gates_tools_label)} description={t(($) => $.workspace.gates_tools_description)}>
          <Input aria-label={t(($) => $.workspace.gates_tools_label)} className="w-72 font-mono" value={tools} disabled={!canEdit || saving} onChange={(e) => setTools(e.target.value)} onBlur={commit} />
        </SettingsRow>
        <SettingsRow label={t(($) => $.workspace.gates_approvers_label)} description={t(($) => $.workspace.gates_approvers_description)} size="select">
          <Select
            items={[
              { value: GATE_APPROVERS_ANY_MEMBER, label: t(($) => $.workspace.gates_approvers_any_member) },
              { value: GATE_APPROVERS_OWNER_ADMIN, label: t(($) => $.workspace.gates_approvers_owner_admin) },
            ]}
            value={approvers}
            disabled={!canEdit || saving}
            onValueChange={(next) => commitApprovers(next as ApprovalGatesPolicy["approvers"])}
          >
            <SelectTrigger size="sm" className="w-full" aria-label={t(($) => $.workspace.gates_approvers_label)}>
              <SelectValue>
                {approvers === GATE_APPROVERS_OWNER_ADMIN ? t(($) => $.workspace.gates_approvers_owner_admin) : t(($) => $.workspace.gates_approvers_any_member)}
              </SelectValue>
            </SelectTrigger>
            <SelectContent align="end">
              <SelectItem value={GATE_APPROVERS_ANY_MEMBER}>{t(($) => $.workspace.gates_approvers_any_member)}</SelectItem>
              <SelectItem value={GATE_APPROVERS_OWNER_ADMIN}>{t(($) => $.workspace.gates_approvers_owner_admin)}</SelectItem>
            </SelectContent>
          </Select>
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}
