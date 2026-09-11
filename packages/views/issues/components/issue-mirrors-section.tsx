"use client";

import { useQuery } from "@tanstack/react-query";
import { Copy } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import { issueMirrorsOptions, isMirrorOpen, useSetMirrorTypeSynced } from "@multica/core/mirrors";
import { Badge } from "@multica/ui/components/ui/badge";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";

/**
 * Cross-repo mirrors (K54).
 *
 * On a source issue: one chip per mirror, with its project, status and the
 * per-mirror "types synced" marker. On a generated mirror: a banner naming the
 * source it came from. An issue that is neither renders nothing.
 */
export function IssueMirrorsSection({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const workspace = useCurrentWorkspace();
  const { data } = useQuery(issueMirrorsOptions(wsId, issueId));
  const setSynced = useSetMirrorTypeSynced(wsId, issueId);

  const mirrors = data?.mirrors ?? [];
  const source = data?.mirror_of ?? null;
  if (mirrors.length === 0 && !source) return null;

  const issueHref = (id: string) => (workspace?.slug ? `/${workspace.slug}/issues/${id}` : undefined);

  return (
    <div data-testid="issue-mirrors" className="space-y-2">
      {source ? (
        <div
          data-testid="issue-mirror-banner"
          role="note"
          className="flex flex-wrap items-center gap-2 rounded-md border border-dashed px-2 py-1 text-caption text-muted-foreground"
        >
          <Copy className="size-3.5 shrink-0" aria-hidden="true" />
          <span>
            {t(($) => $.mirrors.generated_from, {
              identifier: source.identifier,
              project: source.project_title || t(($) => $.mirrors.unknown_project),
            })}
          </span>
          {issueHref(source.source_issue_id) ? (
            <AppLink href={issueHref(source.source_issue_id)!} className="font-medium text-primary hover:underline">
              {t(($) => $.mirrors.open_source)}
            </AppLink>
          ) : null}
        </div>
      ) : null}

      {mirrors.length > 0 ? (
        <ul className="flex flex-col gap-1">
          {mirrors.map((m) => (
            <li key={m.id} data-testid="issue-mirror-chip" className="flex flex-wrap items-center gap-2 text-caption">
              <Copy className="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
              {issueHref(m.mirror_issue_id) ? (
                <AppLink href={issueHref(m.mirror_issue_id)!} className="font-medium text-primary hover:underline">
                  {t(($) => $.mirrors.mirror_in, { project: m.project_title || t(($) => $.mirrors.unknown_project) })}
                </AppLink>
              ) : (
                <span className="font-medium">
                  {t(($) => $.mirrors.mirror_in, { project: m.project_title || t(($) => $.mirrors.unknown_project) })}
                </span>
              )}
              <span className="font-mono text-muted-foreground">{m.identifier}</span>
              <Badge variant={isMirrorOpen(m) ? "outline" : "secondary"}>{m.status}</Badge>
              <label className="flex items-center gap-1 text-muted-foreground">
                <Checkbox
                  checked={m.type_synced}
                  disabled={setSynced.isPending}
                  onCheckedChange={(checked) =>
                    setSynced.mutate(
                      { mirrorId: m.id, value: checked === true },
                      {
                        onError: (err) =>
                          toast.error(
                            err instanceof Error && err.message ? err.message : t(($) => $.mirrors.sync_failed),
                          ),
                      },
                    )
                  }
                />
                {t(($) => $.mirrors.types_synced)}
              </label>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
