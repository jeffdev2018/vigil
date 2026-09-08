"use client";

import { useId, useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import type { ProjectMemory } from "@multica/core/types";
import { ApiError } from "@multica/core/api";
import { projectMemoryOptions, projectMemoryHistoryOptions, projectMemoryUsageOptions, useRestoreProjectMemory, useUpdateProjectMemory } from "@multica/core/projects";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { useLocale, useT } from "../../i18n";

export function ProjectMemorySection({ wsId, projectId, canEdit }: {
  wsId: string;
  projectId: string;
  canEdit: boolean;
}) {
  const { t } = useT("projects");
  const { data, isPending, isError, refetch } = useQuery(projectMemoryOptions(wsId, projectId));
  const update = useUpdateProjectMemory(wsId, projectId);
  const restore = useRestoreProjectMemory(wsId, projectId);
  const [historyOpen, setHistoryOpen] = useState(false);
  const history = useInfiniteQuery({ ...projectMemoryHistoryOptions(wsId, projectId), enabled: historyOpen && Boolean(wsId && projectId) });
  const usageQuery = useQuery(projectMemoryUsageOptions(wsId, projectId));
  const usage = usageQuery.data;
  const locale = useLocale();
  const [draft, setDraft] = useState<{ text: string; revision: number; expires: string; restoreRevision?: number } | null>(null);
  const busy = update.isPending || restore.isPending;
  const [error, setError] = useState<string | null>(null);
  const inputId = useId();
  const rules = (draft?.text ?? "").split(/\r?\n/).map((line) => line.trim()).filter(Boolean);
  const validExpiry = draft?.restoreRevision !== undefined || !draft?.expires || new Date(draft.expires).getTime() > Date.now();
  const valid = validExpiry && rules.length <= 20 && rules.every((rule) => [...rule].length <= 500);
  const unavailable = isError || (!isPending && (!data || data.revision < 0));

  const publish = () => {
    if (!draft || !valid) return;
    setError(null);
    const callbacks = {
      onSuccess: (saved: ProjectMemory) => {
        if (saved.revision < 0) {
          setError(t(($) => $.memory.save_failed));
          return;
        }
        setDraft(null);
      },
      onError: (err: unknown) => setError(err instanceof ApiError && err.status === 409
        ? t(($) => $.memory.conflict)
        : t(($) => $.memory.save_failed)),
    };
    if (draft.restoreRevision !== undefined) restore.mutate({ revision: draft.restoreRevision, expectedRevision: draft.revision }, callbacks);
    else update.mutate({ rules, expectedRevision: draft.revision, expiresAt: draft.expires ? new Date(draft.expires).toISOString() : null }, callbacks);
  };

  return (
    <section className="space-y-3" aria-label={t(($) => $.memory.title)}>
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-caption font-medium">{t(($) => $.memory.title)}</h3>
        {canEdit && data && data.revision >= 0 && !unavailable && (
          <Button variant="outline" size="sm" disabled={busy} onClick={() => {
            setError(null);
            setDraft({ text: data.rules.join("\n"), revision: data.revision, expires: localDateTime(data.expires_at) });
          }}>{t(($) => $.memory.edit)}</Button>
        )}
      </div>
      <p className="text-caption text-muted-foreground">{t(($) => $.memory.description)}</p>
      {isPending ? <p className="text-caption">{t(($) => $.memory.loading)}</p> : unavailable ? (
        <div role="alert" className="space-y-2 text-caption">
          <p>{t(($) => $.memory.load_failed)}</p>
          <Button variant="outline" size="sm" onClick={() => void refetch()}>{t(($) => $.memory.retry)}</Button>
        </div>
      ) : data && (
        <>
          {data.expires_at && <p role="status" className="text-caption text-muted-foreground">{data.expired
            ? t(($) => $.memory.expired)
            : t(($) => $.memory.expires_on, { date: new Date(data.expires_at).toLocaleString() })}</p>}
          {data.source_review && (
            <div className="space-y-2 rounded-lg border p-3 text-caption">
              <p className="font-medium">{t(($) => $.memory.source_review)}</p>
              <p className="break-words text-muted-foreground">{data.source_review.reviewed_by} · {new Date(data.source_review.reviewed_at).toLocaleString()}</p>
              <p className="whitespace-pre-wrap break-words">{data.source_review.feedback}</p>
            </div>
          )}
          {data.rules.length === 0 ? <p className="text-caption text-muted-foreground">{t(($) => $.memory.empty)}</p> : (
            <ul className="list-disc space-y-2 pl-4 text-caption">
              {data.rules.map((rule, index) => <li key={index} className="break-words">{rule}</li>)}
            </ul>
          )}
          {data.revision > 0 && <p className="text-caption text-muted-foreground">{t(($) => $.memory.revision, { revision: data.revision })}</p>}
          <details open={historyOpen} onToggle={(event) => setHistoryOpen(event.currentTarget.open)} className="space-y-3 text-caption">
            <summary className="cursor-pointer">{t(($) => $.memory.history)}</summary>
            {history.isPending && <p>{t(($) => $.memory.loading)}</p>}
            {history.isError && <div role="alert"><p>{t(($) => $.memory.load_failed)}</p><Button variant="outline" size="sm" onClick={() => void history.refetch()}>{t(($) => $.memory.retry)}</Button></div>}
            {history.data?.pages.flatMap((page) => page.versions).map((version) => <div key={version.revision} className="space-y-2 rounded-lg border p-3">
              <p className="font-medium">{t(($) => $.memory.revision, { revision: version.revision })}{version.reviewed_at && ` · ${new Date(version.reviewed_at).toLocaleString()}`}</p>
              {version.restored_from_revision !== undefined && <p>{t(($) => $.memory.restored_from, { revision: version.restored_from_revision })}</p>}
              {version.expired && <p>{t(($) => $.memory.expired)}</p>}
              {version.rules.length ? <ul className="list-disc space-y-1 pl-4">{version.rules.map((rule, i) => <li key={i} className="break-words">{rule}</li>)}</ul> : <p>{t(($) => $.memory.empty)}</p>}
              {canEdit && version.revision !== data.revision && <Button variant="outline" size="sm" disabled={busy || unavailable} onClick={() => {
                setError(null); setDraft({ text: version.rules.join("\n"), revision: data.revision, expires: localDateTime(version.expires_at), restoreRevision: version.revision });
              }}>{t(($) => $.memory.restore_version, { revision: version.revision })}</Button>}
            </div>)}
            {history.hasNextPage && <Button variant="outline" size="sm" disabled={history.isFetchingNextPage} onClick={() => void history.fetchNextPage()}>{t(($) => $.memory.older_versions)}</Button>}
          </details>
          <section className="space-y-2" aria-label={t(($) => $.memory.usage_title)}>
            <h4 className="text-caption font-medium">{t(($) => $.memory.usage_title)}</h4>
            <p className="text-caption text-muted-foreground">{t(($) => $.memory.usage_hint)}</p>
            {usageQuery.isLoading ? <p className="text-caption">{t(($) => $.memory.loading)}</p> : usageQuery.isError || !usage ? (
              <div role="alert" className="space-y-2 text-caption">
                <p>{t(($) => $.memory.usage_failed)}</p>
                <Button variant="outline" size="sm" onClick={() => void usageQuery.refetch()}>{t(($) => $.memory.retry)}</Button>
              </div>
            ) : (
              <>
                <p className="text-caption text-muted-foreground">{t(($) => $.memory.usage_window, {
                  since: new Date(usage.since).toLocaleString(locale),
                  until: new Date(usage.until).toLocaleString(locale),
                })}</p>
                <p className="text-caption">{t(($) => $.memory.usage_coverage, { total: usage.started_runs, recorded: usage.recorded_runs })}</p>
                <dl className="grid grid-cols-2 gap-3 rounded-lg border p-3 sm:grid-cols-3">
                  {[
                    [t(($) => $.memory.usage_included), usage.runs_with_project_memory],
                    [t(($) => $.memory.usage_empty), usage.recorded_runs - usage.runs_with_project_memory],
                    [t(($) => $.memory.usage_unknown), usage.unrecorded_runs],
                  ].map(([label, count]) => (
                    <div key={String(label)} className="min-w-0">
                      <dt className="text-caption text-muted-foreground">{label}</dt>
                      <dd className="text-title tabular-nums">{count}</dd>
                    </div>
                  ))}
                </dl>
                {usage.versions.length > 0 && (
                  <details>
                    <summary className="cursor-pointer text-caption">{t(($) => $.memory.usage_versions, { count: usage.versions.length })}</summary>
                    <ul className="mt-2 max-h-64 space-y-2 overflow-y-auto rounded-lg border p-3 text-caption">
                      {usage.versions.map((version) => (
                        <li key={`${version.project_id}:${version.revision}`} className="space-y-1">
                          <p>{t(($) => $.memory.usage_version_runs, { revision: version.revision, count: version.prepared_runs })}</p>
                          <p className="text-muted-foreground">{t(($) => $.memory.usage_last_started, { date: new Date(version.last_started_at).toLocaleString(locale) })}</p>
                        </li>
                      ))}
                    </ul>
                  </details>
                )}
              </>
            )}
          </section>
        </>
      )}
      <Dialog open={draft !== null} onOpenChange={(open) => { if (!open && !busy) setDraft(null); }}>
        <DialogContent className="sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>{t(($) => $.memory.title)}</DialogTitle>
            <DialogDescription>{draft?.restoreRevision !== undefined ? t(($) => $.memory.restore_hint, { revision: draft.restoreRevision }) : t(($) => $.memory.editor_hint)}</DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor={inputId}>{t(($) => $.memory.rules)}</Label>
            <Textarea id={inputId} rows={8} maxLength={20040} value={draft?.text ?? ""}
              disabled={busy} readOnly={draft?.restoreRevision !== undefined}
              onChange={(event) => setDraft((current) => current && { ...current, text: event.target.value })} />
            <p className="text-caption text-muted-foreground">{t(($) => $.memory.limit)}</p>
            <Label htmlFor={`${inputId}-expiry`}>{t(($) => $.memory.expiry_label)}</Label>
            <input id={`${inputId}-expiry`} type="datetime-local" className="w-full min-w-0 rounded-md border bg-transparent px-3 py-2 text-sm"
              value={draft?.expires ?? ""} disabled={busy} readOnly={draft?.restoreRevision !== undefined}
              onChange={(event) => setDraft((current) => current && { ...current, expires: event.target.value })} />
            <p className="text-caption text-muted-foreground">{t(($) => $.memory.expiry_hint)}</p>
            {!validExpiry && <p role="alert" className="text-caption text-destructive">{t(($) => $.memory.invalid_expiry)}</p>}
            {!valid && validExpiry && <p role="alert" className="text-caption text-destructive">{t(($) => $.memory.invalid)}</p>}
            {error && <p role="alert" className="text-caption text-destructive">{error}</p>}
          </div>
          <DialogFooter>
            <Button variant="ghost" disabled={busy} onClick={() => setDraft(null)}>{t(($) => $.memory.cancel)}</Button>
            <Button disabled={!valid || busy} onClick={publish}>{draft?.restoreRevision !== undefined ? t(($) => $.memory.restore) : t(($) => $.memory.publish)}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}

function localDateTime(value: string | null | undefined) {
  if (!value) return "";
  const date = new Date(value);
  return new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16);
}

/**
 * Promote a delivery correction into shared project memory. Owner/admin only
 * on the server; non-admins see the API 403 as a localized failure.
 */
export function TeachProjectFromReviewButton({
  wsId,
  projectId,
  review,
}: {
  wsId: string;
  projectId: string;
  review: { id: string; feedback: string };
}) {
  const { t } = useT("projects");
  const { data } = useQuery(projectMemoryOptions(wsId, projectId));
  const update = useUpdateProjectMemory(wsId, projectId);
  const [open, setOpen] = useState(false);
  const [saved, setSaved] = useState(false);
  const [text, setText] = useState(review.feedback);
  const [error, setError] = useState<string | null>(null);
  const inputId = useId();
  const rules = text.split(/\r?\n/).map((line) => line.trim()).filter(Boolean);
  const valid = rules.length > 0 && rules.length <= 20 && rules.every((rule) => [...rule].length <= 500);
  const busy = update.isPending;
  const unavailable = !data || data.revision < 0;

  return (
    <>
      {saved ? (
        <p role="status" className="text-caption">{t(($) => $.memory.review_promoted)}</p>
      ) : (
        <Button variant="outline" size="sm" onClick={() => { setError(null); setText(review.feedback); setOpen(true); }}>
          {t(($) => $.memory.review_promote_action)}
        </Button>
      )}
      <Dialog open={open} onOpenChange={(next) => { if (!busy) setOpen(next); }}>
        <DialogContent className="sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>{t(($) => $.memory.review_promote_title)}</DialogTitle>
            <DialogDescription>{t(($) => $.memory.review_promote_hint)}</DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor={inputId}>{t(($) => $.memory.rules)}</Label>
            <Textarea
              id={inputId}
              rows={6}
              maxLength={20040}
              value={text}
              disabled={busy || unavailable}
              onChange={(event) => setText(event.target.value)}
            />
            <p className="text-caption text-muted-foreground">{t(($) => $.memory.limit)}</p>
            {!valid && <p role="alert" className="text-caption text-destructive">{t(($) => $.memory.invalid)}</p>}
            {unavailable && <p role="alert" className="text-caption text-destructive">{t(($) => $.memory.load_failed)}</p>}
            {error && <p role="alert" className="text-caption text-destructive">{error}</p>}
          </div>
          <DialogFooter>
            <Button variant="ghost" disabled={busy} onClick={() => setOpen(false)}>{t(($) => $.memory.cancel)}</Button>
            <Button
              disabled={!valid || busy || unavailable}
              onClick={() => {
                if (!data || !valid || busy) return;
                setError(null);
                update.mutate(
                  { rules, expectedRevision: data.revision, sourceReviewId: review.id },
                  {
                    onSuccess: (savedMemory) => {
                      if (savedMemory.revision < 0 || savedMemory.source_review?.review_id !== review.id) {
                        setError(t(($) => $.memory.save_failed));
                        return;
                      }
                      setSaved(true);
                      setOpen(false);
                    },
                    onError: (err) => setError(
                      err instanceof ApiError && err.status === 409
                        ? t(($) => $.memory.review_promote_conflict)
                        : err instanceof ApiError && err.status === 403
                          ? t(($) => $.memory.review_promote_forbidden)
                          : t(($) => $.memory.save_failed),
                    ),
                  },
                );
              }}
            >
              {t(($) => $.memory.review_promote_publish)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
