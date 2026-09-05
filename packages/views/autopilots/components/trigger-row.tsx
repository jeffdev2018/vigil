"use client";

import { useState } from "react";
import {
  Zap, Clock, Trash2, Webhook, RotateCw, Pencil, FlaskConical,
  ChevronDown, ChevronRight, Ban,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import {
  useCreateAutopilotTrigger,
  useDeleteAutopilotTrigger,
  useRotateAutopilotTriggerWebhookToken,
  useUpdateAutopilotTrigger,
} from "@multica/core/autopilots/mutations";
import { buildAutopilotWebhookUrl, scheduleTriggerDryRunOptions } from "@multica/core/autopilots";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { ScheduleEditor } from "./schedule-editor/schedule-editor";
import { WebhookUrlField } from "./webhook-url-field";
import { getDefaultScheduleConfig, type ScheduleConfig } from "./schedule-editor/model";
import { browserTimezone } from "../../common/timezone-select";
import { cronFields, parseCron, toCron } from "./schedule-editor/cron-mapping";
import { useDescribeSchedule } from "./schedule-editor/describe";
import { formatInTimeZone } from "../../common/format-in-time-zone";
import { SegmentedToggle } from "../../common/segmented-toggle";
import { useScheduleSubmitGate } from "./schedule-editor/validate";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { WebhookEventFilterSection } from "./webhook-event-filter-section";
import { WebhookDryRunDialog } from "./webhook-dry-run-dialog";
import { SigningSecretSection } from "./signing-secret-section";
import { useDeliveryReasonLabel } from "./delivery-reason";
import { effectiveWindowMinutes } from "./schedule-editor/model";
import type {
  AutopilotTrigger,
  UpdateAutopilotTriggerRequest,
  WebhookEventFilter,
} from "@multica/core/types";
import { useT } from "../../i18n";

export function TriggerRow({ trigger, autopilotId, canWrite }: { trigger: AutopilotTrigger; autopilotId: string; canWrite: boolean }) {
  const { t, i18n } = useT("autopilots");
  const describeSchedule = useDescribeSchedule();
  const deleteTrigger = useDeleteAutopilotTrigger();
  const rotateToken = useRotateAutopilotTriggerWebhookToken();
  const updateTrigger = useUpdateAutopilotTrigger();
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [rotateOpen, setRotateOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [dryRunOpen, setDryRunOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);

  // Disabling a trigger is the reversible half of deleting one: an autopilot
  // that fires too often, or a webhook whose sender is misbehaving, can be
  // stopped without losing the URL or the schedule.
  const handleToggleEnabled = async (enabled: boolean) => {
    try {
      await updateTrigger.mutateAsync({ autopilotId, triggerId: trigger.id, enabled });
      toast.success(
        enabled
          ? t(($) => $.trigger_row.toast_enabled)
          : t(($) => $.trigger_row.toast_disabled),
      );
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.trigger_row.toast_toggle_failed),
      );
    }
  };

  const handleDelete = async () => {
    setDeleting(true);
    try {
      await deleteTrigger.mutateAsync({ autopilotId, triggerId: trigger.id });
      toast.success(t(($) => $.trigger_row.toast_deleted));
      setConfirmOpen(false);
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.trigger_row.toast_delete_failed),
      );
    } finally {
      setDeleting(false);
    }
  };

  const isWebhook = trigger.kind === "webhook";
  const isApi = trigger.kind === "api";
  // Resolve the URL from the server's webhook_url first, then compose
  // from the API base URL (desktop) or window.origin (web). Falls back
  // to the relative path if neither is available.
  const webhookUrl = isWebhook
    ? buildAutopilotWebhookUrl({
        trigger,
        apiBaseUrl: api.getBaseUrl(),
        currentOrigin: typeof window !== "undefined" ? window.location.origin : undefined,
      })
    : null;

  const handleRotate = async () => {
    try {
      await rotateToken.mutateAsync({ autopilotId, triggerId: trigger.id });
      toast.success(t(($) => $.trigger_row.toast_rotated));
      setRotateOpen(false);
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.trigger_row.toast_rotate_failed),
      );
    }
  };

  const Icon = isWebhook ? Webhook : isApi ? Zap : Clock;
  const showWebhookUrlRow = isWebhook && webhookUrl;
  // null when the expression is beyond the structured model — those rows keep
  // showing the raw cron on its own.
  // The band lives in its own column, not in the cron text, so parseCron
  // always hands back windowMinutes: 0. Merging the stored band back in is what
  // makes the row read "sometime between 08:00 and 10:00" instead of "at 08:00".
  const scheduleConfig = trigger.cron_expression
    ? {
        ...parseCron(trigger.cron_expression, trigger.timezone ?? "UTC"),
        windowMinutes: trigger.window_minutes ?? 0,
      }
    : null;
  const scheduleDescription = scheduleConfig ? describeSchedule(scheduleConfig) : null;

  // Delete control extracted so a webhook trigger can render it inline
  // with Copy / Rotate on the URL action row (where the other action
  // buttons live), while schedule / api triggers — which have no URL row
  // — keep it pinned to the row's top-right corner. Without this the
  // trash icon visually floats above the URL action buttons because the
  // outer flex uses `items-start`.
  const editButton = canWrite ? (
    <Button
      size="icon"
      variant="ghost"
      className="h-7 w-7 shrink-0"
      onClick={() => setEditOpen(true)}
      title={t(($) => $.trigger_row.edit_trigger)}
    >
      <Pencil className="h-3.5 w-3.5 text-muted-foreground" />
    </Button>
  ) : null;

  // The classifier runs for real on a dry-run, so this is a write-gated
  // action even though it records nothing.
  const dryRunButton = canWrite && isWebhook ? (
    <Button
      size="icon"
      variant="ghost"
      className="h-7 w-7 shrink-0"
      onClick={() => setDryRunOpen(true)}
      title={t(($) => $.dry_run.open)}
    >
      <FlaskConical className="h-3.5 w-3.5 text-muted-foreground" />
    </Button>
  ) : null;

  const deleteButton = canWrite ? (
    <Button
      size="icon"
      variant="ghost"
      className="h-7 w-7 shrink-0"
      onClick={() => setConfirmOpen(true)}
      title={t(($) => $.trigger_row.delete_dialog.confirm)}
    >
      <Trash2 className="h-3.5 w-3.5 text-muted-foreground" />
    </Button>
  ) : null;

  return (
    <div className="flex items-start gap-3 rounded-md border px-3 py-2">
      <Icon className="h-4 w-4 shrink-0 text-muted-foreground mt-0.5" />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 flex-wrap">
          <span className="text-body font-medium">{t(($) => $.trigger_kind[trigger.kind])}</span>
          {trigger.label && (
            <span className="text-caption text-muted-foreground">({trigger.label})</span>
          )}
          {/* The switch already states this for anyone who can flip it. */}
          {!trigger.enabled && !canWrite && (
            <span className="text-caption bg-muted px-1.5 py-0.5 rounded">
              {t(($) => $.trigger_row.disabled_badge)}
            </span>
          )}
          {isApi && (
            <span className="text-caption bg-muted px-1.5 py-0.5 rounded">
              {t(($) => $.trigger_row.deprecated_badge)}
            </span>
          )}
        </div>
        {trigger.cron_expression && (
          // The plain-language line leads; the raw expression drops to a
          // secondary line so the two never run together as one blob.
          <div className="mt-0.5 space-y-0.5">
            <div className="text-caption text-muted-foreground">
              {scheduleDescription ?? trigger.cron_expression}
              {trigger.timezone && ` (${trigger.timezone})`}
            </div>
            {scheduleDescription !== null && scheduleConfig !== null && (
              // Fields only: the zone already reads out in the sentence above,
              // where a person can use it — same rule as the editor's readback.
              <div className="font-mono text-micro text-muted-foreground">
                {cronFields(scheduleConfig)}
              </div>
            )}
          </div>
        )}
        {trigger.next_run_at && (
          <div className="text-caption text-muted-foreground">
            {t(($) => $.trigger_row.next_label, {
              date: formatInTimeZone(
                trigger.next_run_at,
                trigger.timezone ?? undefined,
                i18n.language,
              ),
            })}
          </div>
        )}
        {trigger.kind === "schedule" && (
          <ScheduleNextRuns autopilotId={autopilotId} triggerId={trigger.id} />
        )}
        {showWebhookUrlRow && (
          <div className="mt-1.5">
            <WebhookUrlField
              url={webhookUrl}
              actions={
                <>
                  {canWrite && (
                    <Button
                      size="icon"
                      variant="ghost"
                      className="h-7 w-7 shrink-0"
                      onClick={() => setRotateOpen(true)}
                      title={t(($) => $.trigger_row.rotate_url)}
                      disabled={rotateToken.isPending}
                    >
                      <RotateCw className={cn("h-3.5 w-3.5 text-muted-foreground", rotateToken.isPending && "animate-spin")} />
                    </Button>
                  )}
                  {dryRunButton}
                  {editButton}
                  {deleteButton}
                </>
              }
            />
          </div>
        )}
      </div>
      {canWrite && (
        <Switch
          className="mt-1 shrink-0"
          size="sm"
          checked={trigger.enabled}
          disabled={updateTrigger.isPending}
          onCheckedChange={handleToggleEnabled}
          aria-label={t(($) => $.trigger_row.enable_aria)}
        />
      )}
      {!showWebhookUrlRow && editButton}
      {!showWebhookUrlRow && deleteButton}
      {/* Mounted only while open: the schedule submit gate's state lives with
          the dialog, and a closed one kept mounted would carry a stale
          rejection into its next opening. */}
      {dryRunOpen && (
        <WebhookDryRunDialog
          open
          onOpenChange={setDryRunOpen}
          autopilotId={autopilotId}
          trigger={trigger}
        />
      )}
      {editOpen && (
        <EditTriggerDialog
          open
          onOpenChange={setEditOpen}
          autopilotId={autopilotId}
          trigger={trigger}
        />
      )}
      <AlertDialog open={confirmOpen} onOpenChange={(v) => { if (!v && !deleting) setConfirmOpen(false); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.trigger_row.delete_dialog.title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.trigger_row.delete_dialog.description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleting}>
              {t(($) => $.trigger_row.delete_dialog.cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDelete}
              disabled={deleting}
              className="bg-destructive text-white hover:bg-destructive/90"
            >
              {deleting
                ? t(($) => $.trigger_row.delete_dialog.deleting)
                : t(($) => $.trigger_row.delete_dialog.confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog open={rotateOpen} onOpenChange={(v) => { if (!v && !rotateToken.isPending) setRotateOpen(false); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.trigger_row.rotate_confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.trigger_row.rotate_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={rotateToken.isPending}>
              {t(($) => $.trigger_row.rotate_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleRotate} disabled={rotateToken.isPending}>
              {rotateToken.isPending
                ? t(($) => $.trigger_row.rotate_in_progress)
                : t(($) => $.trigger_row.rotate_confirm_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

// A saved schedule's next instants come from the server: with a firing band
// the minute is derived from the trigger id, so the client cannot reproduce
// it and a locally-computed "next run" would name a minute that never fires.
// Collapsed by default — a detail page with six schedule triggers should not
// fire six requests nobody asked for.
function ScheduleNextRuns({
  autopilotId,
  triggerId,
}: {
  autopilotId: string;
  triggerId: string;
}) {
  const { t, i18n } = useT("autopilots");
  const wsId = useWorkspaceId();
  const [open, setOpen] = useState(false);
  const { data, isLoading, isError } = useQuery(
    scheduleTriggerDryRunOptions(wsId, autopilotId, triggerId, { enabled: open }),
  );
  const ToggleIcon = open ? ChevronDown : ChevronRight;
  const blockedLabel = useDeliveryReasonLabel(data?.reason_code ?? null);

  return (
    <div className="mt-1">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="flex items-center gap-1 rounded text-caption text-muted-foreground hover:text-foreground transition-colors"
      >
        <ToggleIcon className="h-3.5 w-3.5" />
        {t(($) => $.next_runs.label)}
      </button>
      {open && (
        <div className="mt-1 pl-4.5 space-y-0.5">
          {isLoading ? (
            <div className="text-caption text-muted-foreground">
              {t(($) => $.next_runs.loading)}
            </div>
          ) : isError || !data ? (
            <div className="text-caption text-destructive">
              {t(($) => $.next_runs.unavailable)}
            </div>
          ) : (
            <>
              {data.would_run === false && (
                <div className="flex items-center gap-1.5 text-caption text-muted-foreground">
                  <Ban className="h-3.5 w-3.5 shrink-0" />
                  {t(($) => $.next_runs.blocked, {
                    reason: blockedLabel ?? t(($) => $.next_runs.blocked_unknown),
                  })}
                </div>
              )}
              {data.next_runs.length === 0 ? (
                <div className="text-caption text-muted-foreground">
                  {t(($) => $.next_runs.never)}
                </div>
              ) : (
                data.next_runs.map((at) => (
                  <div key={at} className="text-caption text-muted-foreground tabular-nums">
                    {formatInTimeZone(at, undefined, i18n.language)}
                  </div>
                ))
              )}
              {data.window_minutes > 0 && (
                <div className="text-micro text-muted-foreground">
                  {t(($) => $.next_runs.window_hint, { minutes: data.window_minutes })}
                </div>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}

export function AddTriggerDialog({
  open,
  onOpenChange,
  autopilotId,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  autopilotId: string;
}) {
  const { t } = useT("autopilots");
  const wsId = useWorkspaceId();
  const createTrigger = useCreateAutopilotTrigger();
  const [kind, setKind] = useState<"schedule" | "webhook">("schedule");
  const [config, setConfig] = useState<ScheduleConfig>(() =>
    getDefaultScheduleConfig(browserTimezone()),
  );
  const [label, setLabel] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const scheduleGate = useScheduleSubmitGate(wsId);
  const canSubmit = !submitting && (kind !== "schedule" || scheduleGate.scheduleValid);

  const handleSubmit = async () => {
    if (!canSubmit) return;
    setSubmitting(true);
    try {
      if (kind === "schedule") {
        if (!(await scheduleGate.ensureAccepted(config))) {
          setSubmitting(false);
          return;
        }
        const cronExpr = toCron(config);
        if (!cronExpr.trim()) {
          setSubmitting(false);
          return;
        }
        await createTrigger.mutateAsync({
          autopilotId,
          kind: "schedule",
          cron_expression: cronExpr,
          timezone: config.timezone || undefined,
          // The band is not in the cron text — it is its own field, and the
          // editor's "sometime between" control writes it. Omitted (not 0) when
          // there is none, so the server keeps its own default.
          window_minutes: config.windowMinutes || undefined,
          label: label.trim() || undefined,
        });
        toast.success(t(($) => $.add_trigger_dialog.toast_added_schedule));
      } else {
        await createTrigger.mutateAsync({
          autopilotId,
          kind: "webhook",
          label: label.trim() || undefined,
        });
        toast.success(t(($) => $.add_trigger_dialog.toast_added_webhook));
      }
      onOpenChange(false);
      setKind("schedule");
      setConfig(getDefaultScheduleConfig(browserTimezone()));
      setLabel("");
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.add_trigger_dialog.toast_add_failed),
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm">
        <DialogTitle>{t(($) => $.add_trigger_dialog.title)}</DialogTitle>
        {/* DialogContent is a grid, so without min-w-0 this item's min-width is
            its content's — and the cron readback is one unbreakable line that
            would push the track past the dialog instead of truncating. */}
        <div className="min-w-0 space-y-4 pt-2">
          <div>
            <label className="text-caption font-medium text-muted-foreground">
              {t(($) => $.add_trigger_dialog.type_label)}
            </label>
            <div className="mt-1">
              <SegmentedToggle
                value={kind}
                onChange={setKind}
                buttonClassName="px-3 py-1.5 text-body"
                options={[
                  [
                    "schedule",
                    <span key="schedule" className="flex items-center justify-center gap-1.5">
                      <Clock className="h-3.5 w-3.5" />
                      {t(($) => $.add_trigger_dialog.type_schedule)}
                    </span>,
                  ],
                  [
                    "webhook",
                    <span key="webhook" className="flex items-center justify-center gap-1.5">
                      <Webhook className="h-3.5 w-3.5" />
                      {t(($) => $.add_trigger_dialog.type_webhook)}
                    </span>,
                  ],
                ]}
              />
            </div>
          </div>

          {kind === "schedule" ? (
            <ScheduleEditor
              value={config}
              onChange={(next) => {
                scheduleGate.clearRejection();
                setConfig(next);
              }}
              wsId={wsId}
              onValidityChange={scheduleGate.onValidityChange}
              // Same reason as the autopilot dialog: the submit path reads the
              // schedule, validates it over the network, then writes what it
              // read — an edit landing inside that window would be discarded.
              disabled={submitting}
            />
          ) : (
            <p className="rounded-md bg-muted/50 px-3 py-2 text-caption text-muted-foreground">
              {t(($) => $.add_trigger_dialog.webhook_help)}
            </p>
          )}

          <div>
            <label className="text-caption font-medium text-muted-foreground">
              {t(($) => $.add_trigger_dialog.label_field)}
            </label>
            <input
              type="text"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              placeholder={t(($) => $.add_trigger_dialog.label_placeholder)}
              className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-body outline-none focus:ring-1 focus:ring-ring"
            />
          </div>
          <div className="flex justify-end pt-1">
            <Button size="sm" onClick={handleSubmit} disabled={!canSubmit}>
              {submitting
                ? t(($) => $.add_trigger_dialog.submitting)
                : t(($) => $.add_trigger_dialog.submit)}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}


// Editing a trigger in place. Before this, an autopilot's routing rules were
// only reachable through AutopilotDialog, which speaks for `triggers[0]` alone:
// the second webhook on an autopilot could be created and deleted but never
// adjusted, and a schedule's cron could not be corrected without recreating the
// row (and its next_run_at, and — for webhooks — its URL).
export function EditTriggerDialog({
  open,
  onOpenChange,
  autopilotId,
  trigger,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  autopilotId: string;
  trigger: AutopilotTrigger;
}) {
  const { t } = useT("autopilots");
  const wsId = useWorkspaceId();
  const updateTrigger = useUpdateAutopilotTrigger();
  const isSchedule = trigger.kind === "schedule";
  const isWebhook = trigger.kind === "webhook";

  const [config, setConfig] = useState<ScheduleConfig>(() =>
    trigger.cron_expression
      ? {
          ...parseCron(trigger.cron_expression, trigger.timezone ?? "UTC"),
          windowMinutes: trigger.window_minutes ?? 0,
        }
      : getDefaultScheduleConfig(browserTimezone()),
  );
  const [label, setLabel] = useState(trigger.label ?? "");
  const [eventFilters, setEventFilters] = useState<WebhookEventFilter[]>(
    trigger.event_filters ?? [],
  );
  const [eventCriteria, setEventCriteria] = useState(trigger.event_match_criteria ?? "");
  const [submitting, setSubmitting] = useState(false);
  const scheduleGate = useScheduleSubmitGate(wsId);
  const canSubmit = !submitting && (!isSchedule || scheduleGate.scheduleValid);

  const handleSubmit = async () => {
    if (!canSubmit) return;
    setSubmitting(true);
    try {
      // Label is sent unconditionally — "" is how the API clears one, and the
      // field is a plain text box with no other way to say "remove it".
      const patch: UpdateAutopilotTriggerRequest = { label: label.trim() };
      if (isSchedule) {
        if (!(await scheduleGate.ensureAccepted(config))) {
          setSubmitting(false);
          return;
        }
        const cronExpr = toCron(config);
        if (!cronExpr.trim()) {
          setSubmitting(false);
          return;
        }
        patch.cron_expression = cronExpr;
        patch.timezone = config.timezone;
        patch.window_minutes = effectiveWindowMinutes(config);
      }
      if (isWebhook) {
        // Both are authoritative here: an empty list clears the filters and an
        // empty string clears the criteria, which is what the emptied controls
        // mean.
        patch.event_filters = eventFilters;
        patch.event_match_criteria = eventCriteria.trim();
      }
      await updateTrigger.mutateAsync({ autopilotId, triggerId: trigger.id, ...patch });
      toast.success(t(($) => $.edit_trigger_dialog.toast_saved));
      onOpenChange(false);
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.edit_trigger_dialog.toast_save_failed),
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {/* Same min-w-0 as the add dialog: the cron readback is one unbreakable
          line that would otherwise push the grid track past the dialog. */}
      <DialogContent className="max-w-sm">
        <DialogTitle>{t(($) => $.edit_trigger_dialog.title)}</DialogTitle>
        <div className="min-w-0 space-y-4 pt-2">
          {isSchedule && (
            <ScheduleEditor
              value={config}
              onChange={(next) => {
                scheduleGate.clearRejection();
                setConfig(next);
              }}
              wsId={wsId}
              onValidityChange={scheduleGate.onValidityChange}
              disabled={submitting}
            />
          )}
          {isWebhook && (
            <>
              <WebhookEventFilterSection filters={eventFilters} onChange={setEventFilters} />
              <SigningSecretSection autopilotId={autopilotId} trigger={trigger} />
              <div className="space-y-1.5">
                <label
                  htmlFor="edit-trigger-event-criteria"
                  className="text-caption font-medium text-muted-foreground"
                >
                  {t(($) => $.dialog.event_criteria_label)}
                </label>
                <Textarea
                  id="edit-trigger-event-criteria"
                  value={eventCriteria}
                  onChange={(e) => setEventCriteria(e.target.value)}
                  placeholder={t(($) => $.dialog.event_criteria_placeholder)}
                  maxLength={500}
                  rows={3}
                  className="text-body"
                />
                <p className="text-caption text-muted-foreground leading-relaxed">
                  {t(($) => $.dialog.event_criteria_hint)}
                </p>
              </div>
            </>
          )}
          <div>
            <label
              htmlFor="edit-trigger-label"
              className="text-caption font-medium text-muted-foreground"
            >
              {t(($) => $.add_trigger_dialog.label_field)}
            </label>
            <input
              id="edit-trigger-label"
              type="text"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              placeholder={t(($) => $.add_trigger_dialog.label_placeholder)}
              className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-body outline-none focus:ring-1 focus:ring-ring"
            />
          </div>
          <div className="flex justify-end pt-1">
            <Button size="sm" onClick={handleSubmit} disabled={!canSubmit}>
              {submitting
                ? t(($) => $.edit_trigger_dialog.submitting)
                : t(($) => $.edit_trigger_dialog.submit)}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
