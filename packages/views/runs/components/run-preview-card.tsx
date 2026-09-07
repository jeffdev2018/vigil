"use client";

import { useState } from "react";
import { Copy, ExternalLink, Globe, Loader2, MonitorSmartphone, TriangleAlert, X } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  isPreviewLocalOnly,
  isPreviewOpenable,
  previewNeedsShareLink,
  truncatePreviewUrl,
  useCreateTaskShareLink,
  useRevokeTaskShareLink,
  useRunPreview,
  useTaskShareLinks,
} from "@multica/core/runs";
import { Button } from "@multica/ui/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { useT, useTimeAgo } from "../../i18n";

// The preview panel on a run's detail (F12).
//
// Renders nothing when the run has no preview: most runs declare no `run`
// script, and an empty "Preview" heading on every run would be a section that
// exists to say it has nothing to say.
//
// The one thing this component must never do is show a link a reviewer cannot
// open. A loopback preview names the machine instead; a preview whose status
// this build does not know shows the status and no link at all.

const EXPIRY_CHOICES = [1, 24, 168] as const;

export function RunPreviewCard({ taskId, machineName }: { taskId: string; machineName?: string }) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const { data: preview } = useRunPreview(wsId, taskId);
  const { data: links } = useTaskShareLinks(wsId, taskId);
  const createLink = useCreateTaskShareLink(wsId, taskId);
  const revokeLink = useRevokeTaskShareLink(wsId, taskId);
  const [hours, setHours] = useState<number>(24);

  if (!preview || !preview.status) return null;

  const openable = isPreviewOpenable(preview);
  const localOnly = isPreviewLocalOnly(preview);
  const needsLink = previewNeedsShareLink(preview);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(preview.url);
      toast.success(t(($) => $.run_preview.copied));
    } catch {
      toast.error(t(($) => $.run_preview.copy_failed));
    }
  };

  const create = async () => {
    try {
      await createLink.mutateAsync({ capabilities: ["preview"], expires_in_hours: hours });
    } catch (err) {
      toast.error(err instanceof Error && err.message ? err.message : t(($) => $.run_preview.create_link_failed));
    }
  };

  const revoke = async (id: string) => {
    try {
      await revokeLink.mutateAsync(id);
      toast.success(t(($) => $.run_preview.revoked));
    } catch (err) {
      toast.error(err instanceof Error && err.message ? err.message : t(($) => $.run_preview.revoke_failed));
    }
  };

  return (
    <div data-testid="run-preview-card" className="rounded-md border border-border p-3 text-caption">
      <div className="mb-2 flex items-center gap-1 font-medium">
        <StatusIcon status={preview.status} localOnly={localOnly} />
        <span>{t(($) => $.run_preview.card_title)}</span>
      </div>

      <StatusLine preview={preview} localOnly={localOnly} machineName={machineName} />

      {openable && (
        <div className="mt-2 flex items-center gap-2">
          {/* Truncated for the layout, copied in full: a reviewer pastes the
              whole URL into a phone, and a shortened copy would be useless. */}
          <span className="min-w-0 truncate font-mono text-micro text-muted-foreground" title={preview.url}>
            {truncatePreviewUrl(preview.url)}
          </span>
          <Tooltip>
            <TooltipTrigger
              render={<button type="button" onClick={() => void copy()} aria-label={t(($) => $.run_preview.copy)} />}
              className="ml-auto flex items-center justify-center rounded p-1 text-muted-foreground transition-colors hover:bg-accent/50 hover:text-foreground"
            >
              <Copy className="h-3.5 w-3.5" />
            </TooltipTrigger>
            <TooltipContent>{t(($) => $.run_preview.copy)}</TooltipContent>
          </Tooltip>
          {!localOnly && (
            <a
              href={preview.url}
              target="_blank"
              rel="noreferrer noopener"
              data-testid="run-preview-open"
              className="flex items-center gap-1 rounded px-1 py-0.5 text-info transition-colors hover:bg-accent/50"
            >
              {t(($) => $.run_preview.open)}
              <ExternalLink className="h-3 w-3" aria-hidden="true" />
            </a>
          )}
        </div>
      )}

      {needsLink && (
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <span className="text-muted-foreground">{t(($) => $.run_preview.expiry_label)}</span>
          {EXPIRY_CHOICES.map((choice) => (
            <button
              key={choice}
              type="button"
              onClick={() => setHours(choice)}
              data-active={hours === choice ? "true" : undefined}
              className={cn(
                "rounded border border-border px-1.5 py-0.5 transition-colors hover:bg-accent/50",
                // The selected chip stays identifiable while hovered: weight
                // is a dimension hover does not touch.
                hours === choice && "border-info font-medium text-info",
              )}
            >
              {t(($) => $.run_preview.expiry_hours, { count: choice })}
            </button>
          ))}
          <Button size="sm" variant="outline" disabled={createLink.isPending} onClick={() => void create()}>
            {createLink.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null}
            {t(($) => $.run_preview.create_link)}
          </Button>
        </div>
      )}

      {(links?.links.length ?? 0) > 0 && (
        <div className="mt-3">
          <div className="mb-1 font-medium text-muted-foreground">{t(($) => $.run_preview.links_title)}</div>
          <ul className="flex flex-col gap-1">
            {links?.links.map((link) => (
              <li key={link.id} data-testid="run-preview-link" className="flex items-center gap-2">
                <span className="min-w-0 truncate font-mono text-micro">{link.code}</span>
                <span className="text-muted-foreground">
                  {link.expires_at ? t(($) => $.run_preview.expires_in, { when: timeAgo(link.expires_at) }) : ""}
                </span>
                <span className="text-muted-foreground">{t(($) => $.run_preview.uses, { count: link.use_count })}</span>
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <button
                        type="button"
                        onClick={() => void revoke(link.id)}
                        disabled={revokeLink.isPending}
                        aria-label={t(($) => $.run_preview.revoke)}
                      />
                    }
                    className="ml-auto flex items-center justify-center rounded p-1 text-destructive transition-colors hover:bg-destructive/10 disabled:opacity-50"
                  >
                    <X className="h-3.5 w-3.5" />
                  </TooltipTrigger>
                  <TooltipContent>{t(($) => $.run_preview.revoke)}</TooltipContent>
                </Tooltip>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

function StatusIcon({ status, localOnly }: { status: string; localOnly: boolean }) {
  if (localOnly) return <MonitorSmartphone className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />;
  switch (status) {
    case "starting":
      return <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" aria-hidden="true" />;
    case "stale":
      return <TriangleAlert className="h-3.5 w-3.5 text-warning" aria-hidden="true" />;
    case "error":
      return <TriangleAlert className="h-3.5 w-3.5 text-destructive" aria-hidden="true" />;
    default:
      return <Globe className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />;
  }
}

function StatusLine({
  preview,
  localOnly,
  machineName,
}: {
  preview: { status: string; port: number; error: string };
  localOnly: boolean;
  machineName?: string;
}) {
  const { t } = useT("issues");
  if (localOnly) {
    return (
      <p className="text-muted-foreground">
        {machineName
          ? t(($) => $.run_preview.card_local, { machine: machineName })
          : t(($) => $.run_preview.card_local_no_machine)}
      </p>
    );
  }
  switch (preview.status) {
    case "starting":
      return <p className="text-muted-foreground">{t(($) => $.run_preview.card_starting, { port: preview.port })}</p>;
    case "stopped":
      return <p className="text-muted-foreground">{t(($) => $.run_preview.card_stopped)}</p>;
    case "stale":
      return <p className="text-warning">{t(($) => $.run_preview.card_stale)}</p>;
    case "error":
      return (
        <div>
          <p className="text-destructive">{t(($) => $.run_preview.card_error)}</p>
          {/* The tail of the run script's log, verbatim: it is the only thing
              that says what to fix. */}
          {preview.error && (
            <pre className="mt-1 max-h-32 overflow-auto whitespace-pre-wrap rounded bg-muted p-2 font-mono text-micro">
              {preview.error}
            </pre>
          )}
        </div>
      );
    case "ready":
      return null;
    default:
      // A status a newer server added: name it rather than pretending the
      // preview is in a state this build understands.
      return <p className="text-muted-foreground">{preview.status}</p>;
  }
}
