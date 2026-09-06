"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { AlertTriangle, ExternalLink, PauseCircle, RefreshCw } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { linearLinkOptions, useResyncLinearLink } from "@multica/core/linear";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

/**
 * Linear Bridge (K21): the badge that says this issue is a mirror of a Linear
 * issue — its identifier, a link out, and what state the sync is in. When the
 * sync is broken it also offers the manual resync, which re-reads the Linear
 * issue and clears the failure if it works.
 *
 * Renders nothing when the issue has no Linear link, which is the common case.
 */
export function LinearLinkBadge({ issueId }: { issueId: string }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data: link } = useQuery(linearLinkOptions(wsId, issueId));
  const resync = useResyncLinearLink(wsId, issueId);

  if (!link) return null;

  // A state from a newer server falls through to the neutral "synced" copy
  // rather than rendering an empty tooltip.
  const state =
    link.sync_state === "broken" ? "broken" : link.sync_state === "paused" ? "paused" : "active";
  const description = {
    active: t(($) => $.linear.badge_synced),
    paused: t(($) => $.linear.badge_paused),
    broken: link.last_error || t(($) => $.linear.badge_broken),
  }[state];

  async function handleResync() {
    try {
      await resync.mutateAsync(link!.id);
      toast.success(t(($) => $.linear.toast_resynced));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.linear.toast_resync_failed));
    }
  }

  return (
    <div
      data-testid="linear-link-badge"
      data-sync-state={state}
      className={cn(
        "flex flex-wrap items-center gap-2 rounded-md border p-2 text-caption",
        state === "broken" ? "border-destructive/50" : "border-border",
      )}
    >
      {state === "broken" ? (
        <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-destructive" aria-hidden="true" />
      ) : state === "paused" ? (
        <PauseCircle className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
      ) : (
        <ExternalLink className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
      )}
      <span className="font-medium">{t(($) => $.linear.badge_title)}</span>
      {link.linear_url ? (
        <a
          href={link.linear_url}
          target="_blank"
          rel="noreferrer noopener"
          className="underline underline-offset-2"
        >
          {link.linear_issue_identifier || link.linear_issue_id}
        </a>
      ) : (
        <span>{link.linear_issue_identifier || link.linear_issue_id}</span>
      )}
      <span className="min-w-0 flex-1 truncate text-muted-foreground">{description}</span>
      {state === "broken" && (
        <Button size="sm" variant="outline" onClick={handleResync} disabled={resync.isPending}>
          <RefreshCw className={cn("mr-1.5 h-3.5 w-3.5", resync.isPending && "animate-spin")} aria-hidden="true" />
          {resync.isPending ? t(($) => $.linear.badge_resyncing) : t(($) => $.linear.badge_resync)}
        </Button>
      )}
    </div>
  );
}
