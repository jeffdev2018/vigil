"use client";

import { useEffect, useMemo, useState } from "react";
import { Play } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  replayCountsUpTo,
  replayResumable,
  sealState,
  taskReplayOptions,
  useResumeTaskReplay,
  type ReplayEvent,
  type RunReplay,
} from "@multica/core/issues/run-replay";
import type { AgentTask } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { formatTokens, formatUsd } from "../../runtimes/utils";
import { redactSecrets } from "./redact";

/**
 * Run replay (K70): the run as a film. A native range input scrubs through
 * the hash-chained event stream; at each instant the card shows the trace
 * (never the narration alone): what was said, called, changed, asked,
 * handed over, and what it cost so far. The seal badge says whether the
 * chain still matches what the audit log recorded when the run ended.
 * "Resume from here" starts a new run with the trace up to that instant and
 * a new instruction.
 */

const KIND_TONE: Record<string, string> = {
  tool_use: "bg-primary/10 text-primary",
  tool_result: "bg-muted text-muted-foreground",
  effect: "bg-warning/15 text-warning",
  effect_reversed: "bg-destructive/10 text-destructive",
  error: "bg-destructive/10 text-destructive",
  steer: "bg-accent text-accent-foreground",
  decision_asked: "bg-accent text-accent-foreground",
  decision_answered: "bg-accent text-accent-foreground",
  handoff: "bg-warning/15 text-warning",
};

export function ReplayButton({ task, className }: { task: Pick<AgentTask, "id" | "status">; className?: string }) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);
  return (
    <>
      <Tooltip>
        <TooltipTrigger
          render={
            <button
              type="button"
              data-testid="replay-button"
              aria-label={t(($) => $.replay.button)}
              className={cn("rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground", className)}
              onClick={(e) => {
                e.preventDefault();
                e.stopPropagation();
                setOpen(true);
              }}
            >
              <Play aria-hidden className="size-3.5" />
            </button>
          }
        />
        <TooltipContent>{t(($) => $.replay.button)}</TooltipContent>
      </Tooltip>
      {open && <RunReplayDialog taskId={task.id} open={open} onOpenChange={setOpen} />}
    </>
  );
}

export function RunReplayDialog({ taskId: initialTaskId, open, onOpenChange }: { taskId: string; open: boolean; onOpenChange: (open: boolean) => void }) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const [taskId, setTaskId] = useState(initialTaskId);
  const { data, isPending, isError } = useQuery({ ...taskReplayOptions(wsId, taskId), enabled: open });
  const events = useMemo(() => data?.events ?? [], [data]);
  const [pos, setPos] = useState(0);
  useEffect(() => {
    if (events.length > 0) setPos(events.length - 1);
  }, [events.length, taskId]);
  const resume = useResumeTaskReplay(wsId, taskId);
  const [instruction, setInstruction] = useState("");
  const current: ReplayEvent | undefined = events[pos];
  const counts = useMemo(() => replayCountsUpTo(events, pos), [events, pos]);
  const seal = data ? sealState(data) : "unsealed";
  const kindLabel = (k: string) => t(($) => $.replay.kinds[k as keyof typeof $.replay.kinds] ?? k);

  const submitResume = () => {
    if (!current || !instruction.trim()) return;
    resume.mutate(
      { seq: current.seq, instruction: instruction.trim() },
      {
        onSuccess: () => {
          setInstruction("");
          toast.success(t(($) => $.replay.resumed, { seq: current.seq + 1 }));
          onOpenChange(false);
        },
        onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.replay.resume_failed)),
      },
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent data-testid="run-replay" className="flex max-h-[85vh] w-[min(56rem,95vw)] flex-col gap-3 overflow-hidden text-caption">
        <DialogTitle className="text-title">{t(($) => $.replay.title, { agent: data?.run.agent_name || "…" })}</DialogTitle>
        {isPending && <p className="text-muted-foreground">{t(($) => $.replay.loading)}</p>}
        {isError && <p className="text-destructive">{t(($) => $.replay.error)}</p>}
        {data && events.length === 0 && <p className="text-muted-foreground">{t(($) => $.replay.empty)}</p>}
        {data && events.length > 0 && current && (
          <>
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-muted-foreground">
              <span>{t(($) => $.replay.events_count, { n: data.total })}</span>
              <span>{data.run.status}</span>
              <span>
                {formatTokens(data.cost.input_tokens)} / {formatTokens(data.cost.output_tokens)}
                {data.cost.cost_usd_ticks !== null && <> · {formatUsd(data.cost.cost_usd_ticks / 1e10)}</>}
              </span>
              <span
                data-testid="replay-seal"
                data-state={seal}
                className={cn("rounded px-1.5", seal === "verified" && "bg-success/15 text-success", seal === "broken" && "bg-destructive/10 text-destructive", seal === "unsealed" && "bg-muted")}
              >
                {t(($) => $.replay.seal[seal])}
              </span>
            </div>

            {data.run.links.length > 0 && (
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-muted-foreground">{t(($) => $.replay.links)}</span>
                {data.run.links.map((l) => (
                  <button
                    key={l.relation + l.task_id}
                    type="button"
                    data-testid="replay-link"
                    className="rounded border px-2 py-0.5 hover:bg-accent"
                    onClick={() => {
                      setTaskId(l.task_id);
                      setPos(0);
                    }}
                  >
                    {t(($) => $.replay.relations[l.relation as keyof typeof $.replay.relations] ?? l.relation)}
                    {l.agent_name ? ` · ${l.agent_name}` : ""}
                  </button>
                ))}
              </div>
            )}

            <div className="flex items-center gap-2">
              <Button type="button" size="sm" variant="ghost" disabled={pos === 0} onClick={() => setPos((p) => Math.max(0, p - 1))}>
                {t(($) => $.replay.prev)}
              </Button>
              <input
                type="range"
                aria-label={t(($) => $.replay.scrubber)}
                min={0}
                max={events.length - 1}
                value={pos}
                onChange={(e) => setPos(Number(e.target.value))}
                className="flex-1"
              />
              <Button type="button" size="sm" variant="ghost" disabled={pos === events.length - 1} onClick={() => setPos((p) => Math.min(events.length - 1, p + 1))}>
                {t(($) => $.replay.next)}
              </Button>
              <span className="tabular-nums text-muted-foreground">{t(($) => $.replay.position, { pos: pos + 1, total: events.length })}</span>
            </div>

            <div data-testid="replay-event" data-kind={current.kind} className="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto rounded-md border p-3">
              <div className="flex flex-wrap items-center gap-2">
                <span className={cn("rounded px-1.5 font-medium", KIND_TONE[current.kind] ?? "bg-muted")}>{kindLabel(current.kind)}</span>
                <span className="font-medium">{current.title}</span>
                <span className="text-muted-foreground">
                  {current.actor.name || current.actor.type}
                  {" · "}
                  {new Date(current.at).toLocaleTimeString()}
                </span>
              </div>
              {current.text && <pre className="whitespace-pre-wrap break-words font-sans">{redactSecrets(current.text)}</pre>}
              {Object.keys(current.data).length > 0 && (
                <pre className="max-h-64 overflow-auto rounded bg-muted p-2 text-caption">{redactSecrets(JSON.stringify(current.data, null, 2))}</pre>
              )}
              <span className="text-muted-foreground" title={current.hash}>
                {t(($) => $.replay.hash, { hash: current.hash.slice(0, 12) })}
              </span>
            </div>

            <p data-testid="replay-so-far" className="text-muted-foreground">
              {t(($) => $.replay.so_far, { tools: counts.tool_calls, effects: counts.effects, decisions: counts.decisions, steers: counts.steers, handoffs: counts.handoffs })}
            </p>

            {replayResumable(data.run.status) && data.run.issue_id && (
              <div data-testid="replay-resume" className="flex flex-col gap-2 rounded-md border p-3">
                <span className="font-medium">{t(($) => $.replay.resume_title, { pos: pos + 1 })}</span>
                <Textarea rows={2} aria-label={t(($) => $.replay.resume_title, { pos: pos + 1 })} placeholder={t(($) => $.replay.resume_placeholder)} value={instruction} onChange={(e) => setInstruction(e.target.value)} />
                <Button type="button" size="sm" className="self-start" disabled={resume.isPending || !instruction.trim()} onClick={submitResume}>
                  {t(($) => $.replay.resume_button)}
                </Button>
              </div>
            )}
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}

export type { RunReplay };
