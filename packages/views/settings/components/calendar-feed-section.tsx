"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  calendarFeedOptions,
  useDeleteCalendarFeed,
  useSetCalendarFeed,
} from "@multica/core/calendar/queries";
import { api, ApiError } from "@multica/core/api";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { useT } from "../../i18n";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";

/** The window "Check feed" reports on: the next two weeks, the server's cap. */
const CHECK_WITHIN = "336h";

/**
 * Subscribe to a read-only calendar feed (ICS). No OAuth and no write access:
 * the user pastes the URL their calendar already publishes, and Multica reads
 * it to name a recording after the meeting that is actually running.
 *
 * The URL is a capability — anyone holding an ICS link reads the calendar —
 * so it is saved per workspace and only ever shown back to its owner.
 */
export function CalendarFeedSection() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data: feed, isLoading } = useQuery(calendarFeedOptions(wsId));
  const saveFeed = useSetCalendarFeed(wsId);
  const removeFeed = useDeleteCalendarFeed(wsId);
  // null means "showing whatever the server holds"; a string means the user
  // typed. Kept this way rather than seeding through an effect, which would
  // wipe what they were typing the moment the query resolved.
  const [edited, setEdited] = useState<string | null>(null);
  const [checking, setChecking] = useState(false);
  const [checkResult, setCheckResult] = useState<
    { ok: true; count: number } | { ok: false; message: string } | null
  >(null);

  const serverUrl = feed?.url ?? "";
  const url = edited ?? serverUrl;
  const trimmed = url.trim();
  const dirty = trimmed !== serverUrl;
  const busy = saveFeed.isPending || removeFeed.isPending;

  const save = () => {
    setCheckResult(null);
    saveFeed
      .mutateAsync(trimmed)
      .then(() => {
        // Follow the server again: it normalizes webcal:// to https://.
        setEdited(null);
        toast.success(t(($) => $.auto_save.toast_saved), { id: "settings-auto-save" });
      })
      .catch((error) =>
        toast.error(
          error instanceof ApiError && error.message
            ? error.message
            : t(($) => $.preferences.calendar_feed.save_failed),
        ),
      );
  };

  const remove = () => {
    setCheckResult(null);
    removeFeed
      .mutateAsync()
      .then(() => setEdited(null))
      .catch(() => toast.error(t(($) => $.preferences.calendar_feed.save_failed)));
  };

  // Checks what the SERVER holds, not what is in the box: a URL that has not
  // been saved has not been validated, and the server is the only thing that
  // can actually reach the feed.
  const check = async () => {
    setChecking(true);
    setCheckResult(null);
    try {
      const upcoming = await api.calendarUpcoming(CHECK_WITHIN);
      setCheckResult({ ok: true, count: upcoming.events.length });
    } catch (error) {
      setCheckResult({
        ok: false,
        message:
          error instanceof ApiError && error.message
            ? error.message
            : t(($) => $.preferences.calendar_feed.check_failed),
      });
    } finally {
      setChecking(false);
    }
  };

  return (
    <SettingsSection title={t(($) => $.preferences.calendar_title)}>
      <SettingsCard>
        <SettingsRow
          label={t(($) => $.preferences.calendar_feed.title)}
          description={t(($) => $.preferences.calendar_feed.hint)}
          align="start"
          className="flex-col sm:flex-col sm:items-stretch sm:gap-3"
        >
          <div className="flex flex-col gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <Input
                value={url}
                disabled={isLoading || busy}
                placeholder={t(($) => $.preferences.calendar_feed.placeholder)}
                aria-label={t(($) => $.preferences.calendar_feed.title)}
                className="min-w-0 flex-1 font-mono text-caption"
                onChange={(event) => setEdited(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" && dirty && trimmed) save();
                }}
              />
              <Button size="sm" disabled={!dirty || !trimmed || busy} onClick={save}>
                {t(($) => $.preferences.calendar_feed.save)}
              </Button>
              <Button
                size="sm"
                variant="outline"
                disabled={checking || dirty || !feed?.url}
                onClick={() => void check()}
              >
                {checking ? <Spinner className="size-3.5" /> : null}
                {t(($) => $.preferences.calendar_feed.check)}
              </Button>
              {feed?.url ? (
                <Button size="sm" variant="ghost" disabled={busy} onClick={remove}>
                  {t(($) => $.preferences.calendar_feed.remove)}
                </Button>
              ) : null}
            </div>
            {checkResult ? (
              <p
                role="status"
                className={
                  checkResult.ok
                    ? "text-caption text-muted-foreground"
                    : "text-caption text-destructive"
                }
              >
                {checkResult.ok
                  ? t(($) => $.preferences.calendar_feed.check_ok, { count: checkResult.count })
                  : checkResult.message}
              </p>
            ) : feed?.last_error ? (
              // The last automatic read failed: say so here rather than
              // leaving a silent calendar looking like an empty one.
              <p role="status" className="text-caption text-destructive">
                {feed.last_error}
              </p>
            ) : null}
          </div>
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}
