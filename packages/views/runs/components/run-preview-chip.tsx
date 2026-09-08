"use client";

import { ExternalLink, Globe, Loader2, MonitorSmartphone, TriangleAlert } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { isPreviewLocalOnly, isPreviewOpenable, useRunPreview } from "@multica/core/runs";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

// The preview chip on a run row (F12).
//
// Absent, never disabled, when the run has no preview — most runs do not
// declare a `run` script, and a permanently greyed-out control on every row
// would be noise that can never become an affordance.
//
// A `ready` relayed preview is the only state that renders a link. Everything
// else renders a label: a status this build does not recognise still shows,
// because a newer server's state is information, but it never becomes
// something to click.

export function RunPreviewChip({ taskId }: { taskId: string }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: preview } = useRunPreview(wsId, taskId);

  if (!preview || !preview.status) return null;

  const openable = isPreviewOpenable(preview);
  const localOnly = isPreviewLocalOnly(preview);

  const { label, tone, Icon } = chipAppearance(preview.status, localOnly, t);

  // A loopback preview has a URL that only resolves on the machine that ran
  // the task. Rendering it as an anchor in a browser on another machine would
  // be a link that always fails; the desktop app is where it opens.
  if (openable && !localOnly) {
    return (
      <Tooltip>
        <TooltipTrigger
          render={
            <a
              href={preview.url}
              target="_blank"
              rel="noreferrer noopener"
              data-testid="run-preview-chip"
              onClick={(e) => e.stopPropagation()}
            />
          }
          className="flex items-center gap-1 rounded px-1 py-0.5 text-caption text-info transition-colors hover:bg-accent/50"
        >
          <Globe className="h-3.5 w-3.5" aria-hidden="true" />
          <span>{label}</span>
          <ExternalLink className="h-3 w-3" aria-hidden="true" />
        </TooltipTrigger>
        <TooltipContent>{preview.url}</TooltipContent>
      </Tooltip>
    );
  }

  return (
    <Tooltip>
      <TooltipTrigger
        render={<span data-testid="run-preview-chip" />}
        className={cn("flex items-center gap-1 rounded px-1 py-0.5 text-caption", tone)}
      >
        <Icon className={cn("h-3.5 w-3.5", preview.status === "starting" && "animate-spin")} aria-hidden="true" />
        <span>{label}</span>
      </TooltipTrigger>
      <TooltipContent>
        {preview.error ||
          (localOnly ? t(($) => $.run_preview.card_local_no_machine) : label)}
      </TooltipContent>
    </Tooltip>
  );
}

type ChipT = ReturnType<typeof useT<"issues">>["t"];

function chipAppearance(status: string, localOnly: boolean, t: ChipT) {
  if (localOnly) {
    return { label: t(($) => $.run_preview.chip_local), tone: "text-muted-foreground", Icon: MonitorSmartphone };
  }
  switch (status) {
    case "starting":
      return { label: t(($) => $.run_preview.chip_starting), tone: "text-muted-foreground", Icon: Loader2 };
    case "ready":
      return { label: t(($) => $.run_preview.chip_ready), tone: "text-info", Icon: Globe };
    case "stopped":
      return { label: t(($) => $.run_preview.chip_stopped), tone: "text-muted-foreground", Icon: Globe };
    case "stale":
      return { label: t(($) => $.run_preview.chip_stale), tone: "text-warning", Icon: TriangleAlert };
    case "error":
      return { label: t(($) => $.run_preview.chip_error), tone: "text-destructive", Icon: TriangleAlert };
    default:
      // A status a newer server added. Show it verbatim rather than hiding the
      // row's only signal that something is running.
      return { label: status, tone: "text-muted-foreground", Icon: Globe };
  }
}
