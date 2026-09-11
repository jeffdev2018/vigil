"use client";

import { useState } from "react";
import { AlertTriangle } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { runHaltOptions, useSetRunHalt, EMPTY_RUN_HALT } from "@multica/core/run-halt";
import { Input } from "@multica/ui/components/ui/input";
import { Switch } from "@multica/ui/components/ui/switch";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

/**
 * Fleet halt (K05 / m169): stop every agent in the workspace at once. The
 * switch flips the moment it is toggled — never queued behind a save button
 * — the same immediacy as `RunHaltBanner`'s own "Lift the halt": a halt an
 * operator can forget to save is not a halt.
 */
export function RunHaltSetting({ wsId, canEdit }: { wsId: string; canEdit: boolean }) {
  const { t } = useT("settings");
  const { data: halt = EMPTY_RUN_HALT } = useQuery(runHaltOptions(wsId));
  const setRunHalt = useSetRunHalt(wsId);
  const [reason, setReason] = useState(halt.reason);
  const halted = halt.halted === true;

  const commit = (nextHalted: boolean, nextReason: string) => {
    setRunHalt.mutate(
      { halted: nextHalted, reason: nextReason },
      {
        onSuccess: (data) => {
          setReason(data.reason);
          toast.success(t(($) => $.auto_save.toast_saved), { id: "settings-auto-save" });
          if (data.halted && data.frozen_count > 0) {
            toast.success(t(($) => $.workspace.run_halt_frozen_toast, { count: data.frozen_count }));
          } else if (!data.halted && data.resumed_count > 0) {
            toast.success(t(($) => $.workspace.run_halt_resumed_toast, { count: data.resumed_count }));
          }
        },
        onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.workspace.run_halt_failed)),
      },
    );
  };

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <AlertTriangle className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.run_halt_section)}
        </span>
      }
      description={t(($) => $.workspace.run_halt_section_description)}
    >
      <SettingsCard>
        <SettingsRow
          label={t(($) => $.workspace.run_halt_enabled_label)}
          description={t(($) => $.workspace.run_halt_enabled_description)}
        >
          <Switch
            aria-label={t(($) => $.workspace.run_halt_enabled_label)}
            checked={halted}
            disabled={!canEdit || setRunHalt.isPending}
            onCheckedChange={(checked: boolean) => commit(checked, reason)}
          />
        </SettingsRow>
        <SettingsRow
          label={t(($) => $.workspace.run_halt_reason_label)}
          description={t(($) => $.workspace.run_halt_reason_description)}
        >
          <Input
            aria-label={t(($) => $.workspace.run_halt_reason_label)}
            className="w-72"
            value={reason}
            disabled={!canEdit || setRunHalt.isPending || !halted}
            onChange={(e) => setReason(e.target.value)}
            onBlur={() => {
              if (halted && reason !== halt.reason) commit(true, reason);
            }}
          />
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}
