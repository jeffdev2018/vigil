"use client";

import { useState } from "react";
import { Receipt } from "lucide-react";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { Workspace } from "@multica/core/types";
import { Input } from "@multica/ui/components/ui/input";
import { Switch } from "@multica/ui/components/ui/switch";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

// The server stores the threshold in cost_usd_ticks (1e-10 USD) so the
// comparison against a run's cost stays integer; the field speaks dollars.
const TICKS_PER_USD = 1e10;
const DEFAULT_THRESHOLD_USD = 5;

/** Stored ticks → the dollar amount the field shows, or null when disabled. */
export function postmortemCostThresholdUsd(
  workspace: Workspace | null | undefined,
): number | null {
  const ticks = workspace?.postmortem_cost_threshold_usd_ticks;
  if (typeof ticks !== "number" || ticks <= 0) return null;
  return ticks / TICKS_PER_USD;
}

/**
 * Costly-run postmortems (k68): a postmortem is always drafted for a run that
 * failed; this arms the second trigger, for a run that SUCCEEDED but cost more
 * than the workspace is willing to spend on one.
 */
export function PostmortemCostSetting({
  workspace,
  canEdit,
}: {
  workspace: Workspace;
  canEdit: boolean;
}) {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const stored = postmortemCostThresholdUsd(workspace);
  const [amount, setAmount] = useState(
    String(stored ?? DEFAULT_THRESHOLD_USD),
  );
  const [saving, setSaving] = useState(false);
  const disabled = !canEdit || saving;

  async function persist(usd: number) {
    if (saving) return;
    setSaving(true);
    try {
      const updated = await api.updateWorkspace(workspace.id, {
        // 0 is the "off" sentinel the endpoint documents.
        postmortem_cost_threshold_usd_ticks: Math.round(usd * TICKS_PER_USD),
      });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
      toast.success(t(($) => $.auto_save.toast_saved), {
        id: "settings-auto-save",
      });
    } catch (e) {
      toast.error(
        e instanceof Error
          ? e.message
          : t(($) => $.workspace.postmortem_cost_failed),
      );
    } finally {
      setSaving(false);
    }
  }

  const commitAmount = () => {
    // A threshold below a hundredth of a cent would fire on every run; clamp
    // rather than silently storing something that behaves like "always".
    const usd = Math.max(0.01, Number(amount) || DEFAULT_THRESHOLD_USD);
    setAmount(String(usd));
    if (stored !== null && usd !== stored) void persist(usd);
  };

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <Receipt className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.postmortem_cost_section)}
        </span>
      }
    >
      <SettingsCard>
        <div data-testid="postmortem-cost-setting">
          <SettingsRow
            label={t(($) => $.workspace.postmortem_cost_enabled)}
            description={t(($) => $.workspace.postmortem_cost_intro)}
          >
            <Switch
              aria-label={t(($) => $.workspace.postmortem_cost_enabled)}
              checked={stored !== null}
              disabled={disabled}
              onCheckedChange={(on) =>
                void persist(
                  on ? Number(amount) || DEFAULT_THRESHOLD_USD : 0,
                )
              }
            />
          </SettingsRow>
          {stored !== null && (
            <SettingsRow
              label={t(($) => $.workspace.postmortem_cost_threshold)}
              description={t(($) => $.workspace.postmortem_cost_threshold_description)}
            >
              <Input
                type="number"
                min={0.01}
                step={0.01}
                className="w-28"
                aria-label={t(($) => $.workspace.postmortem_cost_threshold)}
                value={amount}
                disabled={disabled}
                onChange={(e) => setAmount(e.target.value)}
                onBlur={commitAmount}
              />
            </SettingsRow>
          )}
        </div>
      </SettingsCard>
    </SettingsSection>
  );
}
