"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useQueries, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, FileCode2, Loader2, Upload } from "lucide-react";
import { api } from "@multica/core/api";
import {
  autopilotKeys,
  cronPreviewOptions,
  daemonErrorsByLine,
  daemonScheduleCrons,
  type DaemonImportPreview,
  type DaemonImportStrategy,
} from "@multica/core/autopilots";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { SegmentedToggle } from "../../common/segmented-toggle";
import { useT } from "../../i18n";

const STRATEGIES: DaemonImportStrategy[] = ["fail", "overwrite", "rename"];

/**
 * Imports a DAEMON.md: drop a file or paste the text, see what the SERVER
 * parsed out of it, then write.
 *
 * The preview is a server round-trip, not a local parse. Re-implementing the
 * frontmatter rules here would eventually disagree with the endpoint that
 * actually writes rows, and the disagreement would show up as a document the
 * dialog called valid and the import then refused.
 */
export function DaemonImportDialog({
  open,
  onOpenChange,
  onImported,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onImported?: (autopilotId: string) => void;
}) {
  const { t } = useT("autopilots");
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();

  const [text, setText] = useState("");
  const [strategy, setStrategy] = useState<DaemonImportStrategy>("fail");
  const [preview, setPreview] = useState<DaemonImportPreview | null>(null);
  const [previewing, setPreviewing] = useState(false);
  const [importing, setImporting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [dragging, setDragging] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);

  const reset = useCallback(() => {
    setText("");
    setPreview(null);
    setError(null);
    setStrategy("fail");
  }, []);

  // Debounced preview. A stale verdict beside an edited document reads as an
  // answer to the text currently on screen, so the preview is dropped the
  // moment the text changes and only re-requested once typing settles.
  useEffect(() => {
    if (!open) return;
    const trimmed = text.trim();
    if (trimmed === "") {
      setPreview(null);
      return;
    }
    let cancelled = false;
    setPreviewing(true);
    const timer = setTimeout(() => {
      api
        .previewDaemonImport(text)
        .then((result) => {
          if (!cancelled) setPreview(result);
        })
        .catch((e: unknown) => {
          if (!cancelled) {
            setPreview(null);
            setError(e instanceof Error && e.message ? e.message : t(($) => $.daemon_import.preview_failed));
          }
        })
        .finally(() => {
          if (!cancelled) setPreviewing(false);
        });
    }, 300);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [open, text, t]);

  const readFile = useCallback((file: File) => {
    file
      .text()
      .then((content) => {
        setText(content);
        setError(null);
      })
      .catch(() => setError(t(($) => $.daemon_import.read_failed)));
  }, [t]);

  const errors = preview?.errors ?? [];
  const byLine = daemonErrorsByLine(errors);
  const frontmatter = preview?.frontmatter ?? null;
  const schedules = daemonScheduleCrons(frontmatter?.triggers);
  const conflict = Boolean(preview?.existing_autopilot_id) && preview?.unchanged !== true;

  // Next runs come from the server's cron preview — the same endpoint the
  // schedule editor uses, so the dialog never approximates a cron locally.
  const cronPreviews = useQueries({
    queries: schedules.map((s) =>
      cronPreviewOptions(wsId, s.cron, s.timezone, 0, { enabled: open && preview?.valid === true }),
    ),
  });

  const canImport = preview?.valid === true && !importing && (!conflict || strategy !== "fail");

  const handleImport = async () => {
    if (preview?.valid !== true) return;
    setImporting(true);
    setError(null);
    try {
      const result = await api.importDaemon(text, strategy);
      await queryClient.invalidateQueries({ queryKey: autopilotKeys.all(wsId) });
      onImported?.(result.autopilot.id);
      onOpenChange(false);
      reset();
    } catch (e: unknown) {
      setError(e instanceof Error && e.message ? e.message : t(($) => $.daemon_import.import_failed));
    } finally {
      setImporting(false);
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        onOpenChange(next);
        if (!next) reset();
      }}
    >
      <DialogContent className="max-w-2xl max-h-[85vh] overflow-y-auto">
        <DialogTitle className="flex items-center gap-2">
          <FileCode2 className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.daemon_import.title)}
        </DialogTitle>

        <div className="min-w-0 space-y-4 pt-1">
          <p className="text-caption text-muted-foreground leading-relaxed">
            {t(($) => $.daemon_import.description)}
          </p>

          <div
            onDragOver={(e) => {
              e.preventDefault();
              setDragging(true);
            }}
            onDragLeave={() => setDragging(false)}
            onDrop={(e) => {
              e.preventDefault();
              setDragging(false);
              const file = e.dataTransfer.files?.[0];
              if (file) readFile(file);
            }}
            className={cn(
              "rounded-md border border-dashed p-3 transition-colors",
              dragging ? "border-primary bg-accent/40" : "border-border",
            )}
          >
            <div className="flex items-center justify-between gap-3 pb-2">
              <span className="text-caption text-muted-foreground">
                {t(($) => $.daemon_import.drop_hint)}
              </span>
              <Button size="sm" variant="outline" onClick={() => fileInput.current?.click()}>
                <Upload className="h-3.5 w-3.5 mr-1" />
                {t(($) => $.daemon_import.choose_file)}
              </Button>
              <input
                ref={fileInput}
                type="file"
                accept=".md,text/markdown,text/plain"
                className="hidden"
                onChange={(e) => {
                  const file = e.target.files?.[0];
                  if (file) readFile(file);
                  e.target.value = "";
                }}
              />
            </div>
            <Textarea
              value={text}
              onChange={(e) => {
                setText(e.target.value);
                setPreview(null);
                setError(null);
              }}
              rows={10}
              spellCheck={false}
              placeholder={t(($) => $.daemon_import.placeholder)}
              className="font-mono text-caption"
              aria-label={t(($) => $.daemon_import.textarea_label)}
            />
          </div>

          {previewing && (
            <p className="flex items-center gap-2 text-caption text-muted-foreground">
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
              {t(($) => $.daemon_import.checking)}
            </p>
          )}

          {errors.length > 0 && (
            <div className="space-y-1.5">
              <h3 className="text-caption font-medium text-destructive">
                {t(($) => $.daemon_import.errors_title, { count: errors.length })}
              </h3>
              <ul className="space-y-1">
                {[...byLine.entries()]
                  .sort((a, b) => a[0] - b[0])
                  .flatMap(([line, lineErrors]) =>
                    lineErrors.map((e, i) => (
                      <li key={`${line}-${i}`} className="text-caption text-destructive break-words">
                        {line > 0 && (
                          <span className="font-mono text-muted-foreground mr-1.5">
                            {t(($) => $.daemon_import.line_prefix, { line })}
                          </span>
                        )}
                        {e.message}
                      </li>
                    )),
                  )}
              </ul>
            </div>
          )}

          {frontmatter && preview?.valid === true && (
            <div className="space-y-3 rounded-md border p-3">
              <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1.5 text-caption">
                <dt className="text-muted-foreground">{t(($) => $.daemon_import.field_name)}</dt>
                <dd className="min-w-0 break-words font-medium">{frontmatter.name}</dd>
                <dt className="text-muted-foreground">{t(($) => $.daemon_import.field_agent)}</dt>
                <dd className="min-w-0 break-words">
                  {frontmatter.agent}
                  {preview.agent_id === undefined && (
                    <span className="ml-2 text-destructive">
                      {t(($) => $.daemon_import.agent_unknown)}
                    </span>
                  )}
                </dd>
                <dt className="text-muted-foreground">{t(($) => $.daemon_import.field_outputs)}</dt>
                <dd className="min-w-0">{frontmatter.outputs}</dd>
                <dt className="text-muted-foreground">{t(($) => $.daemon_import.field_role)}</dt>
                <dd className="min-w-0 whitespace-pre-wrap break-words">{frontmatter.role}</dd>
              </dl>

              {frontmatter.triggers.length > 0 && (
                <div className="space-y-1.5">
                  <span className="text-caption font-medium text-muted-foreground">
                    {t(($) => $.daemon_import.triggers_title)}
                  </span>
                  <ul className="space-y-1">
                    {frontmatter.triggers.map((trigger, i) => (
                      <li key={i} className="flex flex-wrap items-center gap-2 text-caption">
                        <Badge variant="secondary">{trigger.kind}</Badge>
                        {trigger.cron !== undefined && trigger.cron !== "" && (
                          <span className="font-mono">{trigger.cron}</span>
                        )}
                        {trigger.label !== undefined && trigger.label !== "" && (
                          <span className="text-muted-foreground">{trigger.label}</span>
                        )}
                      </li>
                    ))}
                  </ul>
                </div>
              )}

              {schedules.length > 0 && (
                <div className="space-y-1">
                  <span className="text-caption font-medium text-muted-foreground">
                    {t(($) => $.daemon_import.next_runs_title)}
                  </span>
                  {schedules.map((s, i) => {
                    const nextRuns = cronPreviews[i]?.data?.next_runs ?? null;
                    return (
                      <p key={i} className="text-caption text-muted-foreground font-mono break-words">
                        {s.cron}
                        {": "}
                        {nextRuns === null || nextRuns.length === 0
                          ? t(($) => $.daemon_import.next_runs_unknown)
                          : nextRuns.slice(0, 3).join(", ")}
                      </p>
                    );
                  })}
                </div>
              )}

              <div className="space-y-1.5">
                <span className="text-caption font-medium text-muted-foreground">
                  {t(($) => $.daemon_import.body_title)}
                </span>
                <pre
                  data-testid="daemon-import-body"
                  className="max-h-40 overflow-auto rounded bg-muted p-2 text-caption font-mono whitespace-pre-wrap break-words"
                >
                  {preview.body}
                </pre>
              </div>
            </div>
          )}

          {(preview?.warnings ?? []).map((warning, i) => (
            <p key={i} className="flex items-start gap-2 text-caption text-muted-foreground">
              <AlertTriangle className="h-3.5 w-3.5 mt-0.5 shrink-0" />
              <span className="min-w-0 break-words">{warning}</span>
            </p>
          ))}

          {preview?.unchanged === true && (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.daemon_import.unchanged)}
            </p>
          )}

          {conflict && (
            <div className="space-y-1.5">
              <span className="text-caption font-medium text-muted-foreground">
                {t(($) => $.daemon_import.strategy_title)}
              </span>
              <SegmentedToggle
                value={strategy}
                onChange={(next) => setStrategy(next as DaemonImportStrategy)}
                buttonClassName="px-2 py-1 text-caption"
                options={STRATEGIES.map(
                  (s) => [s, t(($) => $.daemon_import.strategy[s])] as [string, string],
                )}
              />
              <p className="text-caption text-muted-foreground">
                {t(($) => $.daemon_import.strategy_hint[strategy])}
              </p>
            </div>
          )}

          {error !== null && (
            <p className="text-caption text-destructive break-words">{error}</p>
          )}

          <div className="flex justify-end gap-2 pt-1">
            <Button variant="outline" size="sm" onClick={() => onOpenChange(false)}>
              {t(($) => $.daemon_import.cancel)}
            </Button>
            <Button size="sm" onClick={handleImport} disabled={!canImport}>
              {importing && <Loader2 className="h-3.5 w-3.5 mr-1 animate-spin" />}
              {t(($) => $.daemon_import.submit)}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
