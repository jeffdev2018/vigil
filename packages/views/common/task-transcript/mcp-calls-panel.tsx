"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Cable } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { mcpCallFromReplayData, taskMcpCallsOptions, type TaskMcpCall } from "@multica/core/issues/mcp-calls";
import type { ReplayEvent } from "@multica/core/issues/run-replay";
import type { AgentTask } from "@multica/core/types";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

const RESULT_TONE: Record<string, string> = {
  success: "text-foreground",
  refused: "text-destructive",
  gated: "text-warning",
  secret_refused: "text-destructive",
  remote_error: "text-destructive",
};

/** Compact table of governed MCP calls: tool · class · result · gate_id. */
export function McpCallsPanel({ calls, className }: { calls: TaskMcpCall[]; className?: string }) {
  const { t } = useT("agents");
  if (calls.length === 0) {
    return <p className={cn("text-muted-foreground", className)}>{t(($) => $.mcp_calls.empty)}</p>;
  }
  return (
    <div data-testid="mcp-calls-panel" className={cn("overflow-x-auto", className)}>
      <table className="w-full text-left text-caption">
        <thead>
          <tr className="text-muted-foreground">
            <th className="pr-3 font-medium">{t(($) => $.mcp_calls.col_tool)}</th>
            <th className="pr-3 font-medium">{t(($) => $.mcp_calls.col_class)}</th>
            <th className="pr-3 font-medium">{t(($) => $.mcp_calls.col_result)}</th>
            <th className="pr-3 font-medium">{t(($) => $.mcp_calls.col_gate)}</th>
            <th className="font-medium">{t(($) => $.mcp_calls.col_ms)}</th>
          </tr>
        </thead>
        <tbody>
          {calls.map((c) => (
            <tr key={c.id} data-testid="mcp-call-row" className="border-t border-border/60">
              <td className="py-1 pr-3 font-medium">
                {c.server ? `${c.server}/` : ""}
                {c.tool || "—"}
                {c.risk ? <span className="ml-1 text-muted-foreground">({c.risk})</span> : null}
              </td>
              <td className="py-1 pr-3 text-muted-foreground">{c.class || "—"}</td>
              <td className={cn("py-1 pr-3", RESULT_TONE[c.result] ?? "text-muted-foreground")}>{c.result || "—"}</td>
              <td className="py-1 pr-3 tabular-nums text-muted-foreground">{c.gate_id || "—"}</td>
              <td className="py-1 tabular-nums text-muted-foreground">{c.duration_ms > 0 ? `${c.duration_ms}ms` : "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** MCP calls recorded in the run's replay film (full list for the run UI). */
export function McpCallsFromReplay({ events }: { events: ReplayEvent[] }) {
  const { t } = useT("agents");
  const calls = useMemo(
    () =>
      events
        .filter((e) => e.kind === "mcp_call")
        .map((e) => mcpCallFromReplayData(e.source_id || String(e.seq), e.at, e.data)),
    [events],
  );
  if (calls.length === 0) return null;
  return (
    <div data-testid="replay-mcp-calls" className="rounded-md border p-3">
      <p className="mb-2 font-medium">{t(($) => $.mcp_calls.panel_title, { n: calls.length })}</p>
      <McpCallsPanel calls={calls} />
    </div>
  );
}

/** Past-row affordance: open the MCP call list without loading the full replay. */
export function McpCallsButton({ task, className }: { task: Pick<AgentTask, "id">; className?: string }) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);
  return (
    <>
      <Tooltip>
        <TooltipTrigger
          render={
            <button
              type="button"
              data-testid="mcp-calls-button"
              aria-label={t(($) => $.mcp_calls.button)}
              className={cn("rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground", className)}
              onClick={(e) => {
                e.preventDefault();
                e.stopPropagation();
                setOpen(true);
              }}
            >
              <Cable aria-hidden className="size-3.5" />
            </button>
          }
        />
        <TooltipContent>{t(($) => $.mcp_calls.button)}</TooltipContent>
      </Tooltip>
      {open && <McpCallsDialog taskId={task.id} open={open} onOpenChange={setOpen} />}
    </>
  );
}

function McpCallsDialog({ taskId, open, onOpenChange }: { taskId: string; open: boolean; onOpenChange: (open: boolean) => void }) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const { data: calls = [], isPending, isError } = useQuery({ ...taskMcpCallsOptions(wsId, taskId), enabled: open });
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent data-testid="mcp-calls-dialog" className="flex max-h-[70vh] w-[min(40rem,95vw)] flex-col gap-3 overflow-hidden text-caption">
        <DialogTitle className="text-title">{t(($) => $.mcp_calls.dialog_title)}</DialogTitle>
        {isPending && <p className="text-muted-foreground">{t(($) => $.mcp_calls.loading)}</p>}
        {isError && <p className="text-destructive">{t(($) => $.mcp_calls.error)}</p>}
        {!isPending && !isError && (
          <>
            <p className="text-muted-foreground">{t(($) => $.mcp_calls.panel_title, { n: calls.length })}</p>
            <McpCallsPanel calls={calls} className="min-h-0 flex-1 overflow-y-auto" />
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
