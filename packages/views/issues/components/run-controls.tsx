"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Pause, Play } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueRunStateOptions, usePauseRun, useResumeRun, useSteerRun } from "@multica/core/issues/run-control";
import type { AgentTask } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../../i18n";

/**
 * Pause, steer, resume (K19). Running: a Pause button, then "pause pending"
 * until the daemon stops at a safe boundary. Paused: the instructions left
 * so far, a field for the next one, and Resume — which continues the same
 * session in a follow-up run. Nothing here cancels.
 */
export function RunControls({ issueId, task }: { issueId: string; task: AgentTask }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: state } = useQuery({ ...issueRunStateOptions(wsId, issueId), enabled: task.status === "running" || task.status === "paused" });
  const pause = usePauseRun(wsId, issueId);
  const steer = useSteerRun(wsId, issueId);
  const resume = useResumeRun(wsId, issueId);
  const [text, setText] = useState("");
  const fail = (e: unknown) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.run_control.failed));
  const forThisTask = state?.task_id === task.id ? state : null;
  const pending = task.pause_requested_at != null || forThisTask?.pause_pending === true;

  if (task.status === "running") {
    return pending ? (
      <span data-testid="pause-pending" className="inline-flex items-center gap-1 text-caption text-muted-foreground" title={t(($) => $.run_control.pause_pending_hint)}>
        <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-warning" />
        {t(($) => $.run_control.pause_pending)}
      </span>
    ) : (
      <button
        type="button"
        data-testid="pause-run"
        className="flex items-center justify-center rounded p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-50"
        aria-label={t(($) => $.run_control.pause)}
        title={t(($) => $.run_control.pause_hint)}
        disabled={pause.isPending}
        onClick={() => pause.mutate(undefined, { onError: fail })}
      >
        <Pause className="h-3.5 w-3.5" />
      </button>
    );
  }
  if (task.status !== "paused" || task.resumed_by_task_id) return null;
  const instructions = forThisTask?.instructions ?? [];
  return (
    <div data-testid="paused-run-controls" className="flex w-full flex-col gap-1.5 rounded-md border border-warning/50 p-2 text-caption">
      <div className="font-medium">{t(($) => $.run_control.paused_title)}</div>
      {instructions.length > 0 && (
        <ul className="list-disc pl-4 text-muted-foreground">{instructions.map((i, n) => <li key={n} className="whitespace-pre-wrap">{i}</li>)}</ul>
      )}
      <Textarea aria-label={t(($) => $.run_control.instruction)} placeholder={t(($) => $.run_control.instruction_placeholder)} rows={2} value={text} onChange={(e) => setText(e.target.value)} />
      <div className="flex gap-1">
        <Button type="button" size="sm" variant="outline" disabled={steer.isPending || text.trim() === ""} onClick={() => steer.mutate(text.trim(), { onError: fail, onSuccess: () => setText("") })}>
          {t(($) => $.run_control.send)}
        </Button>
        <Button type="button" size="sm" disabled={resume.isPending || instructions.length === 0} title={instructions.length === 0 ? t(($) => $.run_control.resume_needs_instruction) : undefined} onClick={() => resume.mutate(undefined, { onError: fail })}>
          <Play className="h-3.5 w-3.5" />
          {t(($) => $.run_control.resume)}
        </Button>
      </div>
    </div>
  );
}
