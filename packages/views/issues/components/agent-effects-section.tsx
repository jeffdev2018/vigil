"use client";

import { useQuery } from "@tanstack/react-query";
import { Undo2 } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  effectField,
  effectState,
  groupEffectsByRun,
  issueAgentEffectsOptions,
  useUndoAgentEffect,
  useUndoTask,
  type AgentEffect,
  type UndoReport,
} from "@multica/core/issues/agent-effects";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

/**
 * Undo for agent actions (K69): what each run changed on this issue, newest
 * run first, with a button to take a whole run back and one per effect.
 * Reversed, expired and non-reversible effects stay listed and say so.
 * Hidden when no run touched the issue.
 */
export function AgentEffectsSection({ issueId, canManage = true }: { issueId: string; canManage?: boolean }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data } = useQuery(issueAgentEffectsOptions(wsId, issueId));
  const undoTask = useUndoTask(wsId, issueId);
  const undoEffect = useUndoAgentEffect(wsId, issueId);
  const effects = data?.effects ?? [];
  if (effects.length === 0) return null;
  const runs = groupEffectsByRun(effects);
  const report = (r: UndoReport) => {
    if (r.reversed > 0) toast.success(t(($) => $.agent_effects.undone, { count: r.reversed }));
    if (r.skipped.length > 0) toast.error(t(($) => $.agent_effects.skipped, { count: r.skipped.length, reason: t(($) => $.agent_effects.reasons[reasonKey(r.skipped[0]?.reason ?? "")]) }));
    if (r.breaker.tripped) toast.warning(t(($) => $.agent_effects.breaker, { mode: r.breaker.trust_mode }));
  };
  const onError = (e: unknown) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.agent_effects.failed));
  const busy = undoTask.isPending || undoEffect.isPending;
  return (
    <div data-testid="agent-effects" className="flex flex-col gap-2 rounded-md border p-2 text-caption">
      <div className="flex items-center gap-2 font-medium">
        <Undo2 className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
        <span>{t(($) => $.agent_effects.section)}</span>
        <span className="font-normal text-muted-foreground">{t(($) => $.agent_effects.window, { hours: data?.window_hours ?? 24 })}</span>
      </div>
      {runs.map((run) => (
        <div key={run.task_id} data-testid="agent-effects-run" className="flex flex-col gap-1 border-t pt-1.5 first:border-t-0 first:pt-0">
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">{t(($) => $.agent_effects.run_by, { name: run.agent_name || run.task_id.slice(0, 8) })}</span>
            {canManage && run.pending > 0 && (
              <Button
                type="button"
                size="sm"
                variant="outline"
                className="ml-auto h-6"
                disabled={busy}
                onClick={() => undoTask.mutate(run.task_id, { onSuccess: report, onError })}
              >
                {t(($) => $.agent_effects.undo_run, { count: run.pending })}
              </Button>
            )}
          </div>
          <ul className="flex flex-col gap-0.5">
            {run.effects.map((e) => (
              <EffectRow key={e.id} effect={e} canManage={canManage} busy={busy} onUndo={() => undoEffect.mutate(e.id, { onSuccess: report, onError })} />
            ))}
          </ul>
        </div>
      ))}
    </div>
  );
}

function EffectRow({ effect, canManage, busy, onUndo }: { effect: AgentEffect; canManage: boolean; busy: boolean; onUndo: () => void }) {
  const { t } = useT("issues");
  const state = effectState(effect);
  return (
    <li data-testid="agent-effect" data-kind={effect.kind} data-state={state} className={cn("flex items-center gap-2", state !== "pending" && "text-muted-foreground")}>
      <span className={cn((state === "reversed" || state === "rejected") && "line-through")}>{describeEffect(effect, t)}</span>
      {state !== "pending" && <span className="rounded bg-muted px-1">{t(($) => $.agent_effects.states[state])}</span>}
      {state === "pending" && canManage && (
        <Button type="button" size="sm" variant="ghost" className="ml-auto h-5 px-1.5" disabled={busy} onClick={onUndo}>
          {t(($) => $.agent_effects.undo_single)}
        </Button>
      )}
    </li>
  );
}

type Translate = ReturnType<typeof useT<"issues">>["t"];

function describeEffect(e: AgentEffect, t: Translate): string {
  const from = String(e.before["value"] ?? "");
  const to = String(e.after["value"] ?? "");
  switch (e.kind) {
    case "issue_status":
      return t(($) => $.agent_effects.kinds.issue_status, { from, to });
    case "issue_field": {
      const field = effectField(e);
      if (field === "assignee") return t(($) => $.agent_effects.kinds.issue_assignee);
      return t(($) => $.agent_effects.kinds.issue_field, { field, from: from || "∅", to: to || "∅" });
    }
    case "comment_create":
      return t(($) => $.agent_effects.kinds.comment_create, { excerpt: String(e.after["excerpt"] ?? "") });
    case "comment_update":
      return t(($) => $.agent_effects.kinds.comment_update);
    case "note_create":
      return t(($) => $.agent_effects.kinds.note_create, { title: String(e.after["title"] ?? "") });
    case "note_update":
      return t(($) => $.agent_effects.kinds.note_update, { title: String(e.after["title"] ?? e.before["title"] ?? "") });
    case "note_archive":
      return t(($) => $.agent_effects.kinds.note_archive);
    case "triage_verdict":
      return t(($) => $.agent_effects.kinds.triage_verdict, { verdict: String(e.after["verdict"] ?? "") });
    case "issue_create":
      return t(($) => $.agent_effects.kinds.issue_create, { title: String(e.after["title"] ?? "") });
    case "issue_update":
      return t(($) => $.agent_effects.kinds.issue_update, { fields: Object.keys(e.payload).join(", ") });
    case "comment_delete":
      return t(($) => $.agent_effects.kinds.comment_delete, { excerpt: String(e.before["excerpt"] ?? "") });
    case "note_delete":
      return t(($) => $.agent_effects.kinds.note_delete, { title: String(e.before["title"] ?? "") });
    case "chat_message":
      return t(($) => $.agent_effects.kinds.chat_message, { excerpt: String(e.after["excerpt"] ?? "") });
    default:
      return e.kind;
  }
}

function reasonKey(reason: string): "already_reversed" | "not_reversible" | "window_expired" | "reverse_failed" | "not_applied" {
  if (reason.startsWith("reverse_failed")) return "reverse_failed";
  if (reason === "already_reversed" || reason === "not_reversible" || reason === "window_expired" || reason === "not_applied") return reason;
  return "reverse_failed";
}
