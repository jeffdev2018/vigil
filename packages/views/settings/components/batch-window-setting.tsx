"use client";

import { MoonStar } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  BATCH_WINDOW_DEFAULTS,
  batchWindowOptions,
  batchWindowProblem,
  useUpdateBatchWindow,
  type BatchWindow,
} from "@multica/core/batch-window";
import { Input } from "@multica/ui/components/ui/input";
import { Switch } from "@multica/ui/components/ui/switch";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";
import { TimezoneSelect } from "../../common/timezone-select";
import { useT } from "../../i18n";

/**
 * Off-peak hours (K45). One window per workspace, during which autopilots that
 * declared their work non-urgent are queued behind everything synchronous.
 *
 * The two times render straight from the server value, with no mirrored local
 * state: an `<input type="time">` reports either an empty string or a complete
 * "HH:MM", never a half-typed one, so there is no in-progress edit to hold — and
 * a mirror has to be resynced from the query, which silently discards what the
 * user typed on any refetch that briefly hands back no data.
 */
export function BatchWindowSetting({ canEdit }: { canEdit: boolean }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data: window } = useQuery(batchWindowOptions(wsId));
  const save = useUpdateBatchWindow(wsId);

  const current: BatchWindow = window ?? BATCH_WINDOW_DEFAULTS;

  const commit = (next: BatchWindow) => {
    const problem = batchWindowProblem(next);
    if (problem !== null) {
      toast.error(
        problem === "equal_bounds"
          ? t(($) => $.workspace.batch_window_equal_bounds)
          : t(($) => $.workspace.batch_window_invalid_time),
      );
      return;
    }
    save.mutate(next, {
      onError: (e: unknown) =>
        toast.error(
          e instanceof Error && e.message ? e.message : t(($) => $.workspace.batch_window_failed),
        ),
    });
  };

  // Clearing a field is a step on the way to retyping it, not a window: hold
  // the save until both times are back, instead of erroring on every clear.
  const commitTime = (patch: Partial<BatchWindow>) => {
    const next = { ...current, ...patch };
    if (!next.start_local_time || !next.end_local_time) return;
    commit(next);
  };

  const disabled = !canEdit || save.isPending;

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <MoonStar className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.batch_window_section)}
        </span>
      }
      description={t(($) => $.workspace.batch_window_description)}
    >
      <SettingsCard>
        <SettingsRow
          label={t(($) => $.workspace.batch_window_enabled)}
          description={t(($) => $.workspace.batch_window_enabled_description)}
        >
          <Switch
            aria-label={t(($) => $.workspace.batch_window_enabled)}
            checked={current.enabled === true}
            disabled={disabled}
            onCheckedChange={(checked: boolean) => {
              // Turning the window ON for the first time has no times to send.
              // Offer a default rather than a validation error: 22:00 → 06:00
              // is what almost every workspace means by "off-peak".
              if (checked && batchWindowProblem({ ...current, enabled: true }) !== null) {
                commit({ ...current, enabled: true, start_local_time: "22:00", end_local_time: "06:00" });
                return;
              }
              commit({ ...current, enabled: checked });
            }}
          />
        </SettingsRow>
        <SettingsRow
          label={t(($) => $.workspace.batch_window_hours)}
          description={t(($) => $.workspace.batch_window_hours_description)}
        >
          <div className="flex items-center gap-2">
            <Input
              type="time"
              aria-label={t(($) => $.workspace.batch_window_start)}
              value={current.start_local_time}
              disabled={disabled}
              onChange={(e) => commitTime({ start_local_time: e.target.value })}
              className="w-32"
            />
            <span className="text-caption text-muted-foreground">
              {t(($) => $.workspace.batch_window_to)}
            </span>
            <Input
              type="time"
              aria-label={t(($) => $.workspace.batch_window_end)}
              value={current.end_local_time}
              disabled={disabled}
              onChange={(e) => commitTime({ end_local_time: e.target.value })}
              className="w-32"
            />
          </div>
        </SettingsRow>
        <SettingsRow
          label={t(($) => $.workspace.batch_window_timezone)}
          description={t(($) => $.workspace.batch_window_timezone_description)}
          size="select-wide"
        >
          <TimezoneSelect
            value={current.timezone || "UTC"}
            disabled={disabled}
            browserSuffix={t(($) => $.preferences.timezone.browser_suffix)}
            onValueChange={(next) => commit({ ...current, timezone: next })}
          />
        </SettingsRow>
      </SettingsCard>

      <p role="note" className="px-0.5 text-caption leading-5 text-muted-foreground">
        {current.enabled === true
          ? t(($) => $.workspace.batch_window_active_note)
          : t(($) => $.workspace.batch_window_empty)}
      </p>
    </SettingsSection>
  );
}
