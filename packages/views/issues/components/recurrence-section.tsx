"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Repeat } from "lucide-react";
import { ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { formatCountdown } from "@multica/core/approvals";
import { cronPreviewOptions } from "@multica/core/autopilots";
import {
  DEFAULT_RECURRENCE_HOUR,
  DEFAULT_RECURRENCE_MINUTE,
  DEFAULT_RECURRENCE_MONTH_DAY,
  DEFAULT_RECURRENCE_WEEKDAY,
  dailyCron,
  describeRecurrence,
  formatRecurrenceClock,
  issueRecurrenceOptions,
  localTimezone,
  monthlyCron,
  useClearIssueRecurrence,
  useSetIssueRecurrence,
  weekdaysCron,
  weeklyCron,
  type RecurrenceDescriptor,
  type RecurrencePreset,
} from "@multica/core/recurrence";
import type { IssueRecurrenceOccurrence, IssueRecurrenceResponse } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Switch } from "@multica/ui/components/ui/switch";
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { AppLink } from "../../navigation";
import { useLocale, useT } from "../../i18n";
import { StatusIcon } from "./status-icon";

const PRESET_ORDER: RecurrencePreset[] = [
  "daily",
  "weekdays",
  "weekly",
  "monthly",
  "custom",
  "on_close",
];

/** Sunday-first, matching cron's own day-of-week numbering. */
const WEEKDAYS = [0, 1, 2, 3, 4, 5, 6] as const;

/** 2024-01-07 is a Sunday, so the offset is the cron day-of-week itself. */
function weekdayName(locale: string, dow: number): string {
  return new Intl.DateTimeFormat(locale, { weekday: "long", timeZone: "UTC" }).format(
    new Date(Date.UTC(2024, 0, 7 + dow)),
  );
}

function secondsUntil(iso: string, now = Date.now()): number | null {
  const at = Date.parse(iso);
  return Number.isNaN(at) ? null : Math.max(0, Math.round((at - now) / 1000));
}

/**
 * Recurring issues (OS plan, table stakes): the standing order on this issue —
 * "raise this again every Monday at 09:00". The rule belongs to the series, so
 * the source and every occurrence show and edit the same one; an occurrence
 * additionally says which series it belongs to.
 *
 * Nothing is optimistic: the server owns the resolved next runs and can refuse
 * the write (400 on a bad cron, 403 for a run) — see
 * packages/core/recurrence/mutations.ts.
 */
export function RecurrenceSection({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const locale = useLocale();
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { data, isPending } = useQuery(issueRecurrenceOptions(wsId, issueId));
  const clear = useClearIssueRecurrence(wsId, issueId);
  const [editing, setEditing] = useState(false);
  const [confirmStop, setConfirmStop] = useState(false);
  const setRule = useSetIssueRecurrence(wsId, issueId);

  const rule = data?.recurrence ?? null;
  const descriptor = useMemo<RecurrenceDescriptor | null>(
    () => (rule ? describeRecurrence(rule.mode, rule.cron_expression) : null),
    [rule],
  );

  if (isPending) return null;

  const sentence = descriptor ? recurrenceSentence(descriptor) : "";
  const isOccurrence = data !== null && data !== undefined && data.source.id !== issueId;

  function recurrenceSentence(d: RecurrenceDescriptor): string {
    const tz = rule?.timezone ?? "UTC";
    switch (d.kind) {
      case "daily":
        return t(($) => $.recurrence.sentence.daily, {
          time: formatRecurrenceClock(d.hour, d.minute),
          tz,
        });
      case "weekdays":
        return t(($) => $.recurrence.sentence.weekdays, {
          time: formatRecurrenceClock(d.hour, d.minute),
          tz,
        });
      case "weekly":
        return t(($) => $.recurrence.sentence.weekly, {
          weekday: weekdayName(locale, d.weekday),
          time: formatRecurrenceClock(d.hour, d.minute),
          tz,
        });
      case "monthly":
        return t(($) => $.recurrence.sentence.monthly, {
          day: d.day,
          time: formatRecurrenceClock(d.hour, d.minute),
          tz,
        });
      case "on_close":
        return t(($) => $.recurrence.sentence.on_close);
      // An expression the preset radio cannot represent is shown verbatim
      // rather than paraphrased — a wrong paraphrase would be worse than cron.
      default:
        return t(($) => $.recurrence.sentence.custom, { cron: d.cron });
    }
  }

  return (
    <div
      data-testid="recurrence-section"
      className="flex flex-col gap-2 rounded-md border p-2 text-caption"
    >
      <div className="flex items-center gap-2">
        <Repeat className="size-3.5 text-muted-foreground" aria-hidden="true" />
        <span className="font-medium">{t(($) => $.recurrence.section)}</span>
        <Button
          type="button"
          size="sm"
          variant="ghost"
          className="ml-auto"
          onClick={() => setEditing(true)}
        >
          {rule ? t(($) => $.recurrence.edit) : t(($) => $.recurrence.make_recurring)}
        </Button>
      </div>

      {!rule || !data ? (
        <p className="text-muted-foreground">{t(($) => $.recurrence.empty)}</p>
      ) : (
        <>
          {/* On an occurrence, which series it belongs to comes first: the
              rule is shared, so the person needs to know what they are about
              to edit before they read the rule itself. */}
          {isOccurrence && (
            <p data-testid="recurrence-series-line" className="text-muted-foreground">
              <AppLink
                href={paths.issueDetail(data.source.id)}
                className="underline-offset-2 hover:underline"
              >
                {t(($) => $.recurrence.part_of_series, { identifier: data.source.identifier })}
              </AppLink>
              {data.next_runs[0] ? (
                <>
                  {" · "}
                  {t(($) => $.recurrence.next_on, {
                    when: new Date(data.next_runs[0]!).toLocaleString(locale),
                  })}
                </>
              ) : null}
            </p>
          )}

          <p data-testid="recurrence-sentence" className="font-medium">
            {sentence}
          </p>

          {data.next_runs.length > 0 ? (
            <ul className="flex flex-wrap gap-x-3 text-muted-foreground">
              {data.next_runs.map((at) => {
                const absolute = new Date(at).toLocaleString(locale);
                const countdown = formatCountdown(secondsUntil(at));
                return (
                  <li key={at} className="tabular-nums" title={absolute}>
                    {countdown ? t(($) => $.recurrence.in_time, { countdown }) : absolute}
                  </li>
                );
              })}
            </ul>
          ) : (
            rule.mode !== "on_close" && (
              <p className="text-muted-foreground">{t(($) => $.recurrence.paused)}</p>
            )
          )}

          <div className="flex items-center gap-2">
            <Switch
              checked={rule.enabled}
              disabled={setRule.isPending}
              aria-label={t(($) => $.recurrence.active)}
              onCheckedChange={(checked: boolean) =>
                setRule.mutate(
                  {
                    // The rule is replaced wholesale by the PUT, so the toggle
                    // has to resend what it is not changing.
                    ...(rule.mode === "on_close"
                      ? {}
                      : { cron_expression: rule.cron_expression }),
                    timezone: rule.timezone,
                    mode: rule.mode,
                    enabled: checked,
                  },
                  {
                    onError: (e) => toast.error(saveErrorMessage(e)),
                  },
                )
              }
            />
            {/* Selected reads by weight and tone, dimensions hover does not
                touch, so hovering an active rule never downgrades it. */}
            <span className={rule.enabled ? "font-medium" : "text-muted-foreground"}>
              {t(($) => $.recurrence.active)}
            </span>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              className="ml-auto text-muted-foreground hover:text-destructive"
              onClick={() => setConfirmStop(true)}
            >
              {t(($) => $.recurrence.stop)}
            </Button>
          </div>

          {data.occurrences.length > 0 && (
            <div className="flex flex-col gap-1">
              <span className="text-muted-foreground">{t(($) => $.recurrence.series)}</span>
              <ul className="flex flex-col gap-1">
                {data.occurrences.map((o) => (
                  <OccurrenceRow
                    key={o.id}
                    occurrence={o}
                    isSource={o.id === data.source.id}
                    href={paths.issueDetail(o.id)}
                  />
                ))}
              </ul>
            </div>
          )}
        </>
      )}

      {editing && (
        <RecurrenceDialog
          issueId={issueId}
          current={data ?? null}
          onClose={() => setEditing(false)}
        />
      )}

      {/* Stopping awaits the server: it detaches every past occurrence, and a
          list emptied optimistically would reappear on any failure. */}
      <AlertDialog open={confirmStop} onOpenChange={(open: boolean) => !open && setConfirmStop(false)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.recurrence.stop_dialog.title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.recurrence.stop_dialog.description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.recurrence.stop_dialog.keep)}</AlertDialogCancel>
            <AlertDialogAction
              disabled={clear.isPending}
              onClick={() =>
                clear.mutate(undefined, {
                  onSuccess: () => {
                    setConfirmStop(false);
                    toast.success(t(($) => $.recurrence.stopped));
                  },
                  onError: (e) =>
                    toast.error(
                      e instanceof ApiError && e.status === 403
                        ? t(($) => $.recurrence.forbidden)
                        : e instanceof Error && e.message
                          ? e.message
                          : t(($) => $.recurrence.stop_failed),
                    ),
                })
              }
            >
              {t(($) => $.recurrence.stop_dialog.confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );

  function saveErrorMessage(e: unknown): string {
    if (e instanceof ApiError && e.status === 403) return t(($) => $.recurrence.forbidden);
    return e instanceof Error && e.message ? e.message : t(($) => $.recurrence.save_failed);
  }
}

function OccurrenceRow({
  occurrence,
  isSource,
  href,
}: {
  occurrence: IssueRecurrenceOccurrence;
  isSource: boolean;
  href: string;
}) {
  const { t } = useT("issues");
  const locale = useLocale();
  return (
    <li data-testid="recurrence-occurrence" className="flex items-baseline gap-2">
      <StatusIcon status={occurrence.status} className="size-3.5 shrink-0 self-center" />
      <AppLink href={href} className="flex min-w-0 flex-1 items-baseline gap-2 hover:underline">
        <span className="shrink-0 font-medium tabular-nums">{occurrence.identifier}</span>
        <span className="min-w-0 flex-1 truncate">{occurrence.title}</span>
      </AppLink>
      {isSource && <span className="shrink-0 text-muted-foreground">{t(($) => $.recurrence.source)}</span>}
      <span className="shrink-0 text-muted-foreground">
        {t(($) => $.recurrence.occurrence_created, {
          when: occurrence.created_at ? new Date(occurrence.created_at).toLocaleDateString(locale) : "",
        })}
      </span>
      {occurrence.due_date && (
        <span className="shrink-0 text-muted-foreground">
          {t(($) => $.recurrence.occurrence_due, { when: occurrence.due_date })}
        </span>
      )}
    </li>
  );
}

/**
 * The rhythm, in the terms a person states it: a preset, a time, a timezone.
 * "Custom cron" is the escape hatch, previewed through the server's own
 * evaluator (GET /api/autopilots/cron-preview) so the dialog never guesses at
 * what an expression means.
 */
function RecurrenceDialog({
  issueId,
  current,
  onClose,
}: {
  issueId: string;
  current: IssueRecurrenceResponse | null;
  onClose: () => void;
}) {
  const { t } = useT("issues");
  const locale = useLocale();
  const wsId = useWorkspaceId();
  const save = useSetIssueRecurrence(wsId, issueId);

  const initial = useMemo(
    () =>
      current
        ? describeRecurrence(current.recurrence.mode, current.recurrence.cron_expression)
        : null,
    [current],
  );

  const [preset, setPreset] = useState<RecurrencePreset>(initial?.kind ?? "weekly");
  const [clock, setClock] = useState(
    initial && "hour" in initial
      ? formatRecurrenceClock(initial.hour, initial.minute)
      : formatRecurrenceClock(DEFAULT_RECURRENCE_HOUR, DEFAULT_RECURRENCE_MINUTE),
  );
  const [weekday, setWeekday] = useState(
    initial?.kind === "weekly" ? initial.weekday : DEFAULT_RECURRENCE_WEEKDAY,
  );
  const [monthDay, setMonthDay] = useState(
    initial?.kind === "monthly" ? initial.day : DEFAULT_RECURRENCE_MONTH_DAY,
  );
  const [cron, setCron] = useState(initial?.kind === "custom" ? initial.cron : "");
  const [timezone, setTimezone] = useState(current?.recurrence.timezone ?? localTimezone());
  const [enabled, setEnabled] = useState(current?.recurrence.enabled ?? true);
  const [refusal, setRefusal] = useState("");

  const [hourText = "", minuteText = ""] = clock.split(":");
  const hour = Number(hourText);
  const minute = Number(minuteText);
  const expression = useMemo(() => {
    switch (preset) {
      case "daily":
        return dailyCron(hour, minute);
      case "weekdays":
        return weekdaysCron(hour, minute);
      case "weekly":
        return weeklyCron(weekday, hour, minute);
      case "monthly":
        return monthlyCron(monthDay, hour, minute);
      case "custom":
        return cron.trim();
      default:
        return "";
    }
  }, [preset, hour, minute, weekday, monthDay, cron]);

  const previewable = preset !== "on_close" && expression !== "" && timezone.trim() !== "";
  const preview = useQuery(
    cronPreviewOptions(wsId, expression, timezone.trim(), 0, { enabled: previewable }),
  );

  const canSubmit = !save.isPending && (preset === "on_close" || expression !== "");

  const submit = () => {
    setRefusal("");
    save.mutate(
      preset === "on_close"
        ? { timezone: timezone.trim(), mode: "on_close", enabled }
        : {
            cron_expression: expression,
            timezone: timezone.trim(),
            mode: "schedule",
            enabled,
          },
      {
        onSuccess: () => {
          toast.success(t(($) => $.recurrence.saved));
          onClose();
        },
        onError: (e) => {
          // 400 (bad cron/timezone) and 403 (a run, not a member) both explain
          // why this submit did nothing — they belong in the form, not a toast.
          if (e instanceof ApiError && (e.status === 400 || e.status === 403)) {
            setRefusal(
              e.status === 403
                ? t(($) => $.recurrence.forbidden)
                : e.message || t(($) => $.recurrence.save_failed),
            );
            return;
          }
          toast.error(
            e instanceof Error && e.message ? e.message : t(($) => $.recurrence.save_failed),
          );
        },
      },
    );
  };

  const previewLine = (() => {
    if (!previewable) return "";
    if (preview.isError) return t(($) => $.recurrence.dialog.preview_unreadable);
    const runs = preview.data?.next_runs;
    if (runs === undefined) return "";
    if (runs === null) return t(($) => $.recurrence.dialog.preview_unreadable);
    if (runs.length === 0) return t(($) => $.recurrence.dialog.preview_empty);
    return t(($) => $.recurrence.dialog.preview, {
      runs: runs.map((at) => new Date(at).toLocaleString(locale)).join(" · "),
    });
  })();

  const zones = useMemo(supportedTimezones, []);

  return (
    <Dialog open onOpenChange={(open: boolean) => !open && onClose()}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.recurrence.dialog.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.recurrence.dialog.description)}</DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-3 text-caption">
          <div role="radiogroup" aria-label={t(($) => $.recurrence.dialog.title)} className="flex flex-wrap gap-1.5">
            {PRESET_ORDER.map((p) => (
              <Button
                key={p}
                type="button"
                size="sm"
                role="radio"
                aria-checked={preset === p}
                // Weight carries the choice as well as the fill, so the picked
                // preset stays picked-looking under the cursor.
                variant={preset === p ? "default" : "outline"}
                className={preset === p ? "font-medium" : undefined}
                onClick={() => setPreset(p)}
              >
                {t(($) => $.recurrence.dialog.preset[p])}
              </Button>
            ))}
          </div>

          {preset !== "on_close" && preset !== "custom" && (
            <label className="flex flex-col gap-1">
              <span className="text-muted-foreground">{t(($) => $.recurrence.dialog.time_label)}</span>
              <Input
                type="time"
                aria-label={t(($) => $.recurrence.dialog.time_label)}
                value={clock}
                onChange={(e) => setClock(e.target.value)}
              />
            </label>
          )}

          {preset === "weekly" && (
            <label className="flex flex-col gap-1">
              <span className="text-muted-foreground">{t(($) => $.recurrence.dialog.weekday_label)}</span>
              <Select
                items={WEEKDAYS.map((d) => ({ value: String(d), label: weekdayName(locale, d) }))}
                value={String(weekday)}
                onValueChange={(v: string | null) => {
                  if (v !== null) setWeekday(Number(v) as typeof weekday);
                }}
              >
                <SelectTrigger aria-label={t(($) => $.recurrence.dialog.weekday_label)}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {WEEKDAYS.map((d) => (
                    <SelectItem key={d} value={String(d)}>
                      {weekdayName(locale, d)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </label>
          )}

          {preset === "monthly" && (
            <label className="flex flex-col gap-1">
              <span className="text-muted-foreground">{t(($) => $.recurrence.dialog.day_label)}</span>
              <Input
                type="number"
                min={1}
                max={31}
                aria-label={t(($) => $.recurrence.dialog.day_label)}
                value={String(monthDay)}
                onChange={(e) => setMonthDay(Number(e.target.value))}
              />
            </label>
          )}

          {preset === "custom" && (
            <label className="flex flex-col gap-1">
              <span className="text-muted-foreground">{t(($) => $.recurrence.dialog.cron_label)}</span>
              <Input
                aria-label={t(($) => $.recurrence.dialog.cron_label)}
                placeholder="0 9 * * 1"
                value={cron}
                onChange={(e) => setCron(e.target.value)}
              />
            </label>
          )}

          {preset !== "on_close" && (
            <label className="flex flex-col gap-1">
              <span className="text-muted-foreground">{t(($) => $.recurrence.dialog.timezone_label)}</span>
              {/* A datalist keeps the zone free-text (the server is the
                  authority on what is valid) while sparing the typing. */}
              <Input
                list="recurrence-timezones"
                aria-label={t(($) => $.recurrence.dialog.timezone_label)}
                value={timezone}
                onChange={(e) => setTimezone(e.target.value)}
              />
              <datalist id="recurrence-timezones">
                {zones.map((z) => (
                  <option key={z} value={z} />
                ))}
              </datalist>
            </label>
          )}

          {previewLine && <p className="text-muted-foreground">{previewLine}</p>}

          <div className="flex items-center gap-2">
            <Switch
              checked={enabled}
              aria-label={t(($) => $.recurrence.dialog.enabled_label)}
              onCheckedChange={(checked: boolean) => setEnabled(checked)}
            />
            <span className={enabled ? "font-medium" : "text-muted-foreground"}>
              {t(($) => $.recurrence.dialog.enabled_label)}
            </span>
          </div>

          {refusal && (
            <p role="alert" className="text-destructive">
              {refusal}
            </p>
          )}
        </div>

        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>
            {t(($) => $.recurrence.dialog.cancel)}
          </Button>
          <Button type="button" disabled={!canSubmit} onClick={submit}>
            {t(($) => $.recurrence.dialog.confirm)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** Intl ships the IANA list; an engine without it just loses the suggestions. */
function supportedTimezones(): string[] {
  try {
    const supported = (Intl as { supportedValuesOf?: (key: string) => string[] }).supportedValuesOf;
    return supported ? supported("timeZone") : [];
  } catch {
    return [];
  }
}
