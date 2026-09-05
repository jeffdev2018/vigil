"use client";

import { useEffect, useState } from "react";
import { Undo2 } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { undoSettingsOptions, useSaveUndoSettings, type UndoSettings } from "@multica/core/issues/agent-effects";
import { Input } from "@multica/ui/components/ui/input";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

/** Undo for agent actions (K69): how long an agent's effect stays reversible, and the breaker threshold. */
export function UndoSetting({ canEdit }: { canEdit: boolean }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data: settings } = useQuery(undoSettingsOptions(wsId));
  const save = useSaveUndoSettings(wsId);
  const [draft, setDraft] = useState<UndoSettings>({ window_hours: 24, breaker_threshold: 5 });
  useEffect(() => {
    if (settings) setDraft(settings);
  }, [settings]);
  const persist = (next: UndoSettings) => {
    setDraft(next);
    save.mutate(next, { onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.workspace.undo_failed)) });
  };
  const disabled = !canEdit || save.isPending;
  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <Undo2 className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.undo_section)}
        </span>
      }
    >
      <SettingsCard>
        <SettingsRow label={t(($) => $.workspace.undo_window)} description={t(($) => $.workspace.undo_window_description)}>
          <Input
            type="number"
            min={1}
            max={720}
            aria-label={t(($) => $.workspace.undo_window)}
            className="w-24"
            value={draft.window_hours}
            disabled={disabled}
            onChange={(e) => setDraft({ ...draft, window_hours: Number(e.target.value) })}
            onBlur={() => persist({ ...draft, window_hours: Math.min(720, Math.max(1, Math.round(draft.window_hours) || 24)) })}
          />
        </SettingsRow>
        <SettingsRow label={t(($) => $.workspace.undo_breaker)} description={t(($) => $.workspace.undo_breaker_description)}>
          <Input
            type="number"
            min={0}
            max={100}
            aria-label={t(($) => $.workspace.undo_breaker)}
            className="w-24"
            value={draft.breaker_threshold}
            disabled={disabled}
            onChange={(e) => setDraft({ ...draft, breaker_threshold: Number(e.target.value) })}
            onBlur={() => persist({ ...draft, breaker_threshold: Math.min(100, Math.max(0, Math.round(draft.breaker_threshold) || 0)) })}
          />
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}
