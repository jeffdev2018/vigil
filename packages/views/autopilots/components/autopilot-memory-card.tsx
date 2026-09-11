"use client";

import { useQuery } from "@tanstack/react-query";
import { Brain, Download } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { autopilotMemoryOptions, hasAutopilotMemory } from "@multica/core/autopilots";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useT } from "../../i18n";

/**
 * The daemon's execution memory (F24 / JEF-15), read-only.
 *
 * Read-only is the product rule, not a shortcut: the memory is what the
 * daemon's runs learned, and a human editing it would put words in the run's
 * mouth. The server refuses a member write for the same reason.
 */
export function AutopilotMemoryCard({
  autopilotId,
  lastRunAt,
}: {
  autopilotId: string;
  lastRunAt?: string | null;
}) {
  const { t } = useT("autopilots");
  const wsId = useWorkspaceId();
  const { data, isLoading } = useQuery(autopilotMemoryOptions(wsId, autopilotId));

  const handleExport = async () => {
    try {
      // Browsers cannot be handed a string; the markdown becomes a blob URL
      // that is revoked as soon as the click is dispatched.
      const markdown = await api.exportDaemon(autopilotId);
      const url = URL.createObjectURL(new Blob([markdown], { type: "text/markdown" }));
      const link = document.createElement("a");
      link.href = url;
      link.download = "DAEMON.md";
      link.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      toast.error(
        err instanceof Error && err.message ? err.message : t(($) => $.memory.export_failed),
      );
    }
  };

  return (
    <section className="space-y-3">
      <div className="flex items-baseline justify-between gap-3">
        <h2 className="text-body font-medium text-muted-foreground uppercase tracking-wider">
          {t(($) => $.memory.section_title)}
        </h2>
        <Button size="sm" variant="outline" onClick={handleExport}>
          <Download className="h-3.5 w-3.5 mr-1" />
          {t(($) => $.memory.export_button)}
        </Button>
      </div>

      {isLoading ? (
        <Skeleton className="h-20 w-full" />
      ) : !hasAutopilotMemory(data) ? (
        <div className="rounded-md border border-dashed p-4 text-center">
          <Brain className="mx-auto h-4 w-4 text-muted-foreground" />
          <p className="pt-2 text-body text-muted-foreground">{t(($) => $.memory.empty_title)}</p>
          <p className="text-caption text-muted-foreground">{t(($) => $.memory.empty_hint)}</p>
        </div>
      ) : (
        <div className="space-y-2 rounded-md border p-3">
          {/* Long memories scroll inside the card rather than pushing the run
              history off the page. */}
          <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-words text-caption font-mono">
            {data?.content}
          </pre>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-t pt-2 text-caption text-muted-foreground">
            <span>{t(($) => $.memory.updated_at, { at: data?.updated_at ?? "" })}</span>
            {lastRunAt !== null && lastRunAt !== undefined && lastRunAt !== "" && (
              <span>{t(($) => $.memory.last_run, { at: lastRunAt })}</span>
            )}
            <span className="ml-auto">{t(($) => $.memory.read_only)}</span>
          </div>
        </div>
      )}
    </section>
  );
}
