"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  feedTokenOptions,
  useMintCalendarFeedToken,
  useRevokeCalendarFeedToken,
  useImportGoogleCalendar,
} from "@multica/core/calendar-events";
import { ApiError } from "@multica/core/api";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { copyText } from "@multica/ui/lib/clipboard";
import { useT } from "../../i18n";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";

/**
 * The calendar Multica *publishes* (OS plan, chantier 19) — the counterpart
 * to `CalendarFeedSection`, which is the calendar Multica *reads*. Two
 * blocks: the outbound ICS subscription URL, and a one-shot Google Calendar
 * import through the member's own Composio connection.
 */
export function CalendarPublishSection() {
  const { t } = useT("calendar-events");
  const { t: tSettings } = useT("settings");
  const wsId = useWorkspaceId();
  const { data: status, isLoading, isError, refetch } = useQuery(feedTokenOptions(wsId));
  const mint = useMintCalendarFeedToken(wsId);
  const revoke = useRevokeCalendarFeedToken(wsId);
  const importGoogle = useImportGoogleCalendar(wsId);

  // The clear URL is in the mint response only — held locally until the
  // viewer navigates away, never persisted or re-fetched.
  const [mintedUrl, setMintedUrl] = useState<string | null>(null);
  const [importResult, setImportResult] = useState<
    { ok: true; created: number; updated: number; seen: number } | { ok: false; message: string; code?: string } | null
  >(null);
  const today = new Date();
  const firstOfMonth = new Date(Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), 1));
  const [importFrom, setImportFrom] = useState(firstOfMonth.toISOString().slice(0, 10));
  const [importTo, setImportTo] = useState(today.toISOString().slice(0, 10));

  const runMint = () => {
    mint.mutate(undefined, {
      onSuccess: (result) => setMintedUrl(result.url),
      onError: (err) =>
        toast.error(err instanceof Error && err.message ? err.message : t(($) => $.publish.mint_error)),
    });
  };

  const runRevoke = () => {
    revoke.mutate(undefined, {
      onSuccess: () => setMintedUrl(null),
      onError: () => toast.error(t(($) => $.publish.revoke_error)),
    });
  };

  const runImport = () => {
    setImportResult(null);
    importGoogle.mutate(
      {
        from: new Date(`${importFrom}T00:00:00Z`).toISOString(),
        to: new Date(`${importTo}T23:59:59Z`).toISOString(),
      },
      {
        onSuccess: (result) =>
          setImportResult({ ok: true, created: result.created, updated: result.updated, seen: result.seen }),
        onError: (err) => {
          const code = err instanceof ApiError ? (err.body as { code?: unknown } | undefined)?.code : undefined;
          if (err instanceof ApiError && code === "no_google_connection") {
            setImportResult({ ok: false, message: t(($) => $.publish.google_no_connection), code: "no_google_connection" });
          } else if (err instanceof ApiError && err.status === 503) {
            setImportResult({ ok: false, message: t(($) => $.publish.google_composio_off) });
          } else {
            setImportResult({
              ok: false,
              message: err instanceof Error && err.message ? err.message : t(($) => $.publish.google_import_error),
            });
          }
        },
      },
    );
  };

  return (
    <SettingsSection title={t(($) => $.publish.title)}>
      <SettingsCard>
        <SettingsRow
          label={t(($) => $.publish.subscribe_title)}
          description={t(($) => $.publish.subscribe_hint)}
          align="start"
          className="flex-col sm:flex-col sm:items-stretch sm:gap-3"
        >
          {isError ? (
            <div className="flex flex-col items-start gap-2">
              <p role="alert" className="text-caption text-destructive">
                {t(($) => $.publish.load_error)}
              </p>
              <Button variant="outline" size="sm" onClick={() => void refetch()}>
                {t(($) => $.publish.retry)}
              </Button>
            </div>
          ) : (
          <div className="flex flex-col gap-2">
            {mintedUrl ? (
              <div className="flex flex-wrap items-center gap-2">
                <Input
                  readOnly
                  value={mintedUrl}
                  className="min-w-0 flex-1 font-mono text-caption"
                  onFocus={(e) => e.currentTarget.select()}
                />
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => {
                    void copyText(mintedUrl);
                    toast.success(t(($) => $.publish.copied));
                  }}
                >
                  {t(($) => $.publish.copy)}
                </Button>
              </div>
            ) : null}
            {mintedUrl && (
              <p className="text-caption text-muted-foreground">{t(($) => $.publish.url_shown_once)}</p>
            )}
            <div className="flex flex-wrap items-center gap-2">
              <Button size="sm" disabled={isLoading || mint.isPending} onClick={runMint}>
                {mint.isPending ? <Spinner className="size-3.5" /> : null}
                {status?.configured ? t(($) => $.publish.rotate) : t(($) => $.publish.mint)}
              </Button>
              {status?.configured && (
                <Button size="sm" variant="ghost" disabled={revoke.isPending} onClick={runRevoke}>
                  {t(($) => $.publish.revoke)}
                </Button>
              )}
              {!mintedUrl && status?.configured && status.created_at ? (
                <span className="text-caption text-muted-foreground">
                  {t(($) => $.publish.configured_since, { date: new Date(status.created_at).toLocaleDateString() })}
                </span>
              ) : !mintedUrl && !status?.configured && !isLoading ? (
                <span className="text-caption text-muted-foreground">{t(($) => $.publish.not_configured)}</span>
              ) : null}
            </div>
          </div>
          )}
        </SettingsRow>
      </SettingsCard>

      <SettingsCard className="mt-4">
        <SettingsRow
          label={t(($) => $.publish.google_title)}
          description={t(($) => $.publish.google_hint)}
          align="start"
          className="flex-col sm:flex-col sm:items-stretch sm:gap-3"
        >
          <div className="flex flex-col gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <label className="flex items-center gap-1.5 text-caption text-muted-foreground">
                {t(($) => $.publish.google_from)}
                <input
                  type="date"
                  className="h-8 rounded-md border bg-background px-2 text-body"
                  value={importFrom}
                  onChange={(e) => setImportFrom(e.target.value)}
                />
              </label>
              <label className="flex items-center gap-1.5 text-caption text-muted-foreground">
                {t(($) => $.publish.google_to)}
                <input
                  type="date"
                  className="h-8 rounded-md border bg-background px-2 text-body"
                  value={importTo}
                  onChange={(e) => setImportTo(e.target.value)}
                />
              </label>
              <Button size="sm" disabled={importGoogle.isPending} onClick={runImport}>
                {importGoogle.isPending ? <Spinner className="size-3.5" /> : null}
                {t(($) => $.publish.google_import)}
              </Button>
            </div>
            {importResult && (
              <p
                role="status"
                className={importResult.ok ? "text-caption text-muted-foreground" : "text-caption text-destructive"}
              >
                {importResult.ok
                  ? t(($) => $.publish.google_result, {
                      created: importResult.created,
                      updated: importResult.updated,
                      seen: importResult.seen,
                    })
                  : importResult.code === "no_google_connection"
                    ? (
                      <>
                        {importResult.message}{" "}
                        <a
                          href="?tab=integrations"
                          className="underline decoration-destructive/40 underline-offset-4"
                        >
                          {tSettings(($) => $.page.tabs.integrations)}
                        </a>
                      </>
                    )
                    : importResult.message}
              </p>
            )}
          </div>
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}
