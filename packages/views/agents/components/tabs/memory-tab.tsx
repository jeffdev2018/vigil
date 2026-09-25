"use client";

import { useEffect, useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Brain, MoreHorizontal, Pencil, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import type { Agent, AgentMemory, AgentMemoryVersion, AgentTask } from "@multica/core/types";
import { useAgentPermissions } from "@multica/core/permissions";
import { ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  agentMemoryOptions,
  agentMemoryHistoryOptions,
  agentTasksOptions,
  useCreateAgentMemory,
  useDeleteAgentMemory,
  useUpdateAgentMemory,
} from "@multica/core/agents";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label as FieldLabel } from "@multica/ui/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { MemoryUsageSection } from "./memory-usage-section";
import { MemoryEvaluationsDialog } from "./memory-evaluations-dialog";
import { TranscriptButton } from "../../../common/task-transcript";
import { useT, useTimeAgo } from "../../../i18n";

// Server-side per-agent cap (409 on POST beyond it) — mirrored here only to
// render the "N / 200" counter and to pre-disable the add button.
const MEMORY_LIMIT = 200;
const MEMORY_CONTENT_MAX = 500;

export function MemoryTab({
  agent,
  canEdit = true,
}: {
  agent: Agent;
  canEdit?: boolean;
}) {
  const { t } = useT("agents");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const { data: memoryList, isLoading, isError, refetch } = useQuery(
    agentMemoryOptions(wsId, agent.id),
  );
  const memories = memoryList?.memories ?? [];

  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<AgentMemory | null>(null);
  const [pendingDelete, setPendingDelete] = useState<AgentMemory | null>(null);
  const approve = useUpdateAgentMemory(wsId, agent.id);
  const [approvingId, setApprovingId] = useState<string | null>(null);

  const review = useUpdateAgentMemory(wsId, agent.id);
  const [source, setSource] = useState<AgentMemory | null>(null);
  const [historyId, setHistoryId] = useState<string | null>(null);
  const historyMemory = memories.find((memory) => memory.id === historyId);
  const [evaluationMemoryId, setEvaluationMemoryId] = useState<string | null>(null);
  const evaluationMemory = memories.find((memory) => memory.id === evaluationMemoryId);
  const activeCount = memories.filter((m) => m.status === "active" && !m.expired).length;
  const pendingCount = memories.filter((m) => m.status === "pending").length;
  const reviewMemory = (memory: AgentMemory, status: "active" | "rejected" | "pending") => {
    review.mutate({ memoryId: memory.id, status, expected_revision: memory.revision }, {
      onError: (error) => toast.error(error instanceof ApiError && error.status === 409
        ? t(($) => $.tab_body.memory.conflict_toast)
        : t(($) => $.tab_body.memory.save_failed_toast)),
    });
  };
  const statusLabel = (status: string | undefined) => {
    switch (status) {
      case "pending": return t(($) => $.tab_body.memory.status_pending);
      case "active": return t(($) => $.tab_body.memory.status_active);
      case "rejected": return t(($) => $.tab_body.memory.status_rejected);
      default: return t(($) => $.tab_body.memory.status_unknown);
    }
  };
  const atLimit = memories.length >= MEMORY_LIMIT;

  const approveMemory = (memory: AgentMemory) => {
    setApprovingId(memory.id);
    approve.mutate(
      { memoryId: memory.id, state: "approved" },
      {
        onSettled: () => setApprovingId(null),
        onError: (error) =>
          toast.error(
            error instanceof Error
              ? error.message
              : t(($) => $.tab_body.memory.save_failed_toast),
          ),
      },
    );
  };

  return (
    <div className="space-y-6">
      {evaluationMemory && canEdit && <MemoryEvaluationsDialog wsId={wsId} agentId={agent.id} memory={evaluationMemory} onClose={() => setEvaluationMemoryId(null)} />}
      <p className="text-body leading-6 text-muted-foreground">
        {t(($) => $.tab_body.memory.intro)}
      </p>

      {!isLoading && !isError && <p className="text-caption text-muted-foreground">
        {t(($) => $.tab_body.memory.review_summary, { active: activeCount, pending: pendingCount })}
      </p>}
      <section className="space-y-3">
        <div className="flex items-start justify-between gap-4">
          <div>
            <h3 className="text-body font-medium">
              {t(($) => $.tab_body.memory.list_title)}
            </h3>
            <p className="mt-1 text-caption leading-5 text-muted-foreground">
              {t(($) => $.tab_body.memory.count, {
                count: memories.length,
                max: MEMORY_LIMIT,
              })}
            </p>
          </div>
          {canEdit && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => setEditorOpen(true)}
              disabled={atLimit}
            >
              <Plus className="h-3.5 w-3.5" />
              {t(($) => $.tab_body.memory.add_action)}
            </Button>
          )}
        </div>

        {isLoading ? (
          <div className="rounded-lg border border-dashed px-4 py-10 text-center text-caption text-muted-foreground">
            {t(($) => $.tab_body.memory.loading)}
          </div>
        ) : isError ? (
          <div role="alert" className="space-y-2 rounded-lg border p-4 text-body">
            <p>{t(($) => $.tab_body.memory.load_failed)}</p>
            <Button variant="outline" onClick={() => void refetch()}>{t(($) => $.tab_body.memory.retry_action)}</Button>
          </div>
        ) : memories.length === 0 ? (
          <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-10 text-muted-foreground">
            <span className="opacity-50">
              <Brain className="h-6 w-6" />
            </span>
            <p className="mt-3 text-body">
              {t(($) => $.tab_body.memory.empty_title)}
            </p>
            <p className="mt-1 max-w-sm text-center text-caption">
              {t(($) => $.tab_body.memory.empty_hint)}
            </p>
          </div>
        ) : (
          <ul className="divide-y rounded-lg border bg-surface-raised/40">
            {[...memories].sort((a, b) => Number(b.status === "pending") - Number(a.status === "pending")).map((memory) => (
              <li key={memory.id} className="flex items-start gap-3 p-3">
                <span className="min-w-0 flex-1">
                  <span className="block whitespace-pre-wrap break-words text-body">
                    {memory.content}
                  </span>
                  <span className="mt-1.5 flex flex-wrap items-center gap-2 text-caption text-muted-foreground">
                    <Badge
                      variant={memory.source === "run" ? "secondary" : "outline"}
                    >
                      {memory.source_review ? t(($) => $.tab_body.memory.source_review) : memory.source === "run"
                        ? t(($) => $.tab_body.memory.source_run)
                        : t(($) => $.tab_body.memory.source_manual)}
                    </Badge>
                    {memory.state === "draft" && (
                      <Badge variant="outline" className="border-dashed">
                        {t(($) => $.tab_body.memory.state_draft)}
                      </Badge>
                    )}
                    {!(memory.status === "active" && memory.expired) && <Badge variant="outline">{statusLabel(memory.status)}</Badge>}
                    {memory.expired && <Badge variant="secondary">{t(($) => $.tab_body.memory.expired)}</Badge>}
                    {memory.expires_at && <span>{t(($) => $.tab_body.memory.expires_on, { date: new Date(memory.expires_at).toLocaleString() })}</span>}
                    {memory.updated_at && <span>{timeAgo(memory.updated_at)}</span>}
                    <Button variant="link" size="sm" onClick={() => setHistoryId(memory.id)}>{t(($) => $.tab_body.memory.history_action)}</Button>
                    {canEdit && <Button variant="link" size="sm" onClick={() => setEvaluationMemoryId(memory.id)}>{t(($) => $.tab_body.memory.evaluations.title)}</Button>}
                    {memory.source_task_id && <Button variant="link" size="sm" onClick={() => setSource(memory)}>
                      {t(($) => $.tab_body.memory.source_action)}
                    </Button>}
                  </span>
                  {canEdit && (memory.revision ?? 0) > 0 && (
                    <span className="mt-2 flex flex-wrap gap-2">
                      {memory.status === "pending" && <>
                        <Button size="sm" disabled={review.isPending} onClick={() => reviewMemory(memory, "active")}>{t(($) => $.tab_body.memory.approve_action)}</Button>
                        <Button size="sm" variant="outline" disabled={review.isPending} onClick={() => reviewMemory(memory, "rejected")}>{t(($) => $.tab_body.memory.reject_action)}</Button>
                      </>}
                      {memory.status === "active" && !memory.expired && <Button size="sm" variant="outline" disabled={review.isPending} onClick={() => reviewMemory(memory, "rejected")}>{t(($) => $.tab_body.memory.disable_action)}</Button>}
                      {memory.status === "rejected" && <Button size="sm" variant="outline" disabled={review.isPending} onClick={() => reviewMemory(memory, "pending")}>{t(($) => $.tab_body.memory.reconsider_action)}</Button>}
                    </span>
                  )}
                </span>
                {canEdit && memory.state === "draft" && (
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={approvingId === memory.id && approve.isPending}
                    onClick={() => approveMemory(memory)}
                  >
                    {approvingId === memory.id && approve.isPending
                      ? t(($) => $.tab_body.memory.approve_pending)
                      : t(($) => $.tab_body.memory.approve_action)}
                  </Button>
                )}
                {canEdit && (
                  <DropdownMenu>
                    <DropdownMenuTrigger
                      render={
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={t(($) => $.tab_body.memory.actions_open_aria)}
                        >
                          <MoreHorizontal className="size-4" />
                        </Button>
                      }
                    />
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem onClick={() => setEditing(memory)}>
                        <Pencil className="size-4" />
                        {t(($) => $.tab_body.memory.edit_action)}
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        variant="destructive"
                        onClick={() => setPendingDelete(memory)}
                      >
                        <Trash2 className="size-4" />
                        {t(($) => $.tab_body.memory.delete_action)}
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>

      <MemoryUsageSection wsId={wsId} agentId={agent.id} memories={memories} onHistory={setHistoryId} />

      {historyMemory && <MemoryHistoryDialog wsId={wsId} agentId={agent.id} memory={historyMemory} canEdit={canEdit} statusLabel={statusLabel} onClose={() => setHistoryId(null)} />}

      <MemoryEditorDialog
        wsId={wsId}
        agentId={agent.id}
        open={editorOpen}
        onOpenChange={setEditorOpen}
      />
      <MemoryEditorDialog
        wsId={wsId}
        agentId={agent.id}
        open={Boolean(editing)}
        onOpenChange={(open) => !open && setEditing(null)}
        memory={editing}
      />
      <MemorySourceDialog wsId={wsId} agent={agent} memory={source} onClose={() => setSource(null)} />
      <DeleteMemoryDialog
        wsId={wsId}
        agentId={agent.id}
        memory={pendingDelete}
        onClose={() => setPendingDelete(null)}
      />
    </div>
  );
}

function MemoryEditorDialog({
  wsId,
  agentId,
  open,
  onOpenChange,
  memory,
  sourceTaskId,
  sourceReview,
  onCreated,
}: {
  wsId: string;
  agentId: string;
  sourceTaskId?: string;
  sourceReview?: { id: string; feedback: string };
  onCreated?: (memory: AgentMemory) => void;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  memory?: AgentMemory | null;
}) {
  const { t } = useT("agents");
  const create = useCreateAgentMemory(wsId, agentId);
  const update = useUpdateAgentMemory(wsId, agentId);
  const [content, setContent] = useState("");
  const [expires, setExpires] = useState("");
  const [originalExpires, setOriginalExpires] = useState("");
  const [sourceError, setSourceError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setSourceError(null);
    setContent(memory?.content ?? "");
    const date = memory?.expires_at ? new Date(memory.expires_at) : null;
    const value = date ? new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16) : "";
    setExpires(value);
    setOriginalExpires(value);
  }, [memory, open]);

  const trimmed = content.trim();
  const validExpiry = expires === originalExpires || !expires || new Date(expires).getTime() > Date.now();
  const pending = create.isPending || update.isPending;

  const handleError = (error: unknown, fallback: string) => {
    if (sourceReview) {
      setSourceError(error instanceof ApiError && error.status === 409
        ? t(($) => $.tab_body.memory.review_candidate_conflict)
        : t(($) => $.tab_body.memory.save_failed_toast));
      return;
    }
    if (error instanceof ApiError && error.status === 409) {
      toast.error(memory ? t(($) => $.tab_body.memory.conflict_toast) : t(($) => $.tab_body.memory.cap_reached_toast));
      return;
    }
    toast.error(error instanceof Error ? error.message : fallback);
  };

  const submit = () => {
    if (!trimmed || !validExpiry || pending) return;
    if (memory) {
      update.mutate(
        { memoryId: memory.id, content: trimmed, expected_revision: memory.revision, expires_at: expires === originalExpires ? undefined : expires ? new Date(expires).toISOString() : null },
        {
          onSuccess: () => onOpenChange(false),
          onError: (error) =>
            handleError(error, t(($) => $.tab_body.memory.save_failed_toast)),
        },
      );
      return;
    }
    create.mutate({ content: trimmed, source_task_id: sourceTaskId, expires_at: expires ? new Date(expires).toISOString() : null, source_review_id: sourceReview?.id }, {
      onSuccess: (saved) => { onCreated?.(saved); onOpenChange(false); },
      onError: (error) =>
        handleError(error, t(($) => $.tab_body.memory.save_failed_toast)),
    });
  };

  return (
    <Dialog open={open} onOpenChange={(value) => { if (!pending) onOpenChange(value); }}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {memory
              ? t(($) => $.tab_body.memory.dialog_edit_title)
              : sourceTaskId ? t(($) => $.tab_body.memory.teach_action) : t(($) => $.tab_body.memory.dialog_create_title)}
          </DialogTitle>
          <DialogDescription>
            {sourceReview ? t(($) => $.tab_body.memory.review_candidate_hint) : sourceTaskId ? t(($) => $.tab_body.memory.teach_description) : t(($) => $.tab_body.memory.dialog_description)}
          </DialogDescription>
        </DialogHeader>
        {sourceReview && <p className="max-h-32 overflow-y-auto whitespace-pre-wrap break-words text-caption">{sourceReview.feedback}</p>}
        <div className="space-y-2 py-2">
          <FieldLabel htmlFor="agent-memory-content">
            {t(($) => $.tab_body.memory.content_label)}
          </FieldLabel>
          <Textarea
            id="agent-memory-content"
            autoFocus
            rows={4}
            maxLength={MEMORY_CONTENT_MAX}
            value={content}
            disabled={pending}
            onChange={(event) => setContent(event.target.value)}
            placeholder={t(($) => $.tab_body.memory.content_placeholder)}
          />
          <p className="text-right text-caption tabular-nums text-muted-foreground">
            {t(($) => $.tab_body.memory.char_count, {
              count: content.length,
              max: MEMORY_CONTENT_MAX,
            })}
          </p>
        </div>
        <div className="space-y-2">
          <FieldLabel htmlFor="agent-memory-expiry">{t(($) => $.tab_body.memory.expiry_label)}</FieldLabel>
          <input id="agent-memory-expiry" type="datetime-local" className="w-full min-w-0 rounded-md border bg-transparent px-3 py-2 text-body"
            value={expires} disabled={pending} onChange={(event) => setExpires(event.target.value)} />
          <p className="text-caption text-muted-foreground">{t(($) => $.tab_body.memory.expiry_hint)}</p>
          {!validExpiry && <p role="alert" className="text-caption text-destructive">{t(($) => $.tab_body.memory.invalid_expiry)}</p>}
        </div>
        <DialogFooter>
          <Button variant="ghost" disabled={pending} onClick={() => onOpenChange(false)}>
            {t(($) => $.tab_body.memory.dialog_cancel)}
          </Button>
          <Button onClick={submit} disabled={!trimmed || !validExpiry || pending}>
            {pending
              ? t(($) => $.tab_body.memory.dialog_saving)
              : t(($) => $.tab_body.memory.dialog_save)}
          </Button>
        </DialogFooter>
        {sourceError && <p role="alert" className="text-caption text-destructive">{sourceError}</p>}
      </DialogContent>
    </Dialog>
  );
}

function DeleteMemoryDialog({
  wsId,
  agentId,
  memory,
  onClose,
}: {
  wsId: string;
  agentId: string;
  memory: AgentMemory | null;
  onClose: () => void;
}) {
  const { t } = useT("agents");
  const remove = useDeleteAgentMemory(wsId, agentId);
  return (
    <AlertDialog
      open={Boolean(memory)}
      onOpenChange={(open) => !open && onClose()}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {t(($) => $.tab_body.memory.delete_dialog_title)}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t(($) => $.tab_body.memory.delete_dialog_description)}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>
            {t(($) => $.tab_body.memory.dialog_cancel)}
          </AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={remove.isPending}
            onClick={() => {
              if (!memory) return;
              remove.mutate(memory.id, {
                onSuccess: onClose,
                onError: (error) =>
                  toast.error(
                    error instanceof Error
                      ? error.message
                      : t(($) => $.tab_body.memory.delete_failed_toast),
                  ),
              });
            }}
          >
            {t(($) => $.tab_body.memory.delete_dialog_confirm)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function MemorySourceDialog({ wsId, agent, memory, onClose }: {
  wsId: string; agent: Agent; memory: AgentMemory | null; onClose: () => void;
}) {
  const { t } = useT("agents");
  const { data: tasks = [], isLoading, isError } = useQuery({
    ...agentTasksOptions(wsId, agent.id), enabled: Boolean(memory?.source_task_id),
  });
  const task = tasks.find((item) => item.id === memory?.source_task_id);
  const result = task?.result;
  const output = result && typeof result === "object" && "output" in result && typeof result.output === "string" ? result.output : "";
  return <Dialog open={Boolean(memory)} onOpenChange={(open) => !open && onClose()}>
    <DialogContent className="max-h-[85dvh] overflow-y-auto sm:max-w-2xl">
      <DialogHeader>
        <DialogTitle>{t(($) => $.tab_body.memory.source_action)}</DialogTitle>
        <DialogDescription>{memory?.source_review ? t(($) => $.tab_body.memory.review_source_hint) : t(($) => $.tab_body.memory.source_description)}</DialogDescription>
      </DialogHeader>
      {memory?.source_review && <div className="space-y-3 rounded-lg border p-3 text-body">
        <p className="font-medium">{t(($) => $.tab_body.memory.source_review)}</p>
        <p className="break-words text-caption">{memory.source_review.reviewed_by} · {new Date(memory.source_review.reviewed_at).toLocaleString()}</p>
        <p className="whitespace-pre-wrap break-words">{memory.source_review.feedback}</p>
        <ol className="list-decimal space-y-2 pl-4">{memory.source_review.criteria.map((criterion, i) => <li key={i} className="break-words"><p>{criterion}</p><p className="whitespace-pre-wrap text-caption">{memory.source_review?.assessments[i]?.evidence}</p></li>)}</ol>
      </div>}
      {isLoading ? <p>{t(($) => $.tab_body.memory.loading)}</p> : isError || !task ? <p role="alert">{t(($) => $.tab_body.memory.source_unavailable)}</p> : <>
        <p className="max-h-80 overflow-y-auto whitespace-pre-wrap break-words text-body">{output || t(($) => $.tab_body.memory.source_no_output)}</p>
        <TranscriptButton task={task} agentName={agent.name} title={t(($) => $.tab_body.memory.source_transcript)} />
      </>}
    </DialogContent>
  </Dialog>;
}

export function TeachFromRunButton({ agent, task }: { agent: Agent; task: AgentTask }) {
  const wsId = useWorkspaceId();
  const { canEdit } = useAgentPermissions(agent, wsId);
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);
  if (!canEdit.allowed || task.chat_session_id || !["completed", "failed", "cancelled"].includes(task.status)) return null;
  return <>
    <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.tab_body.memory.teach_action)} title={t(($) => $.tab_body.memory.teach_action)} onClick={() => setOpen(true)}><Brain className="size-3.5" /></Button>
    <MemoryEditorDialog wsId={wsId} agentId={agent.id} sourceTaskId={task.id} open={open} onOpenChange={setOpen} />
  </>;
}

export function TeachFromReviewButton({ wsId, agentId, sourceTaskId, review }: {
  wsId: string; agentId: string; sourceTaskId: string; review: { id: string; feedback: string };
}) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);
  const [saved, setSaved] = useState(false);
  return <>
    {saved ? <p role="status" className="text-caption">{t(($) => $.tab_body.memory.review_candidate_saved)}</p> : <Button variant="outline" size="sm" onClick={() => setOpen(true)}>{t(($) => $.tab_body.memory.review_candidate_action)}</Button>}
    <MemoryEditorDialog wsId={wsId} agentId={agentId} sourceTaskId={sourceTaskId} sourceReview={review} open={open} onOpenChange={setOpen} onCreated={() => setSaved(true)} />
  </>;
}

function MemoryHistoryDialog({ wsId, agentId, memory, canEdit, statusLabel, onClose }: {
  wsId: string;
  agentId: string;
  memory: AgentMemory;
  canEdit: boolean;
  statusLabel: (status: string | undefined) => string;
  onClose: () => void;
}) {
  const { t } = useT("agents");
  const history = useInfiniteQuery(agentMemoryHistoryOptions(wsId, agentId, memory.id));
  const restore = useUpdateAgentMemory(wsId, agentId);
  const [selection, setSelection] = useState<{ version: AgentMemoryVersion; expectedRevision: number } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const recorded = history.data?.pages.flatMap((page) => page.versions) ?? [];
  const versions = selection ? [recorded.find((version) => version.revision === selection.version.revision) ?? selection.version] : recorded;
  const canRestore = canEdit && (memory.revision ?? 0) > 0;
  return (
    <Dialog open onOpenChange={(open) => { if (!open && !restore.isPending) onClose(); }}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t(($) => $.tab_body.memory.history_title)}</DialogTitle>
          <DialogDescription>{selection ? t(($) => $.tab_body.memory.restore_hint) : t(($) => $.tab_body.memory.history_hint)}</DialogDescription>
        </DialogHeader>
        <div className="max-h-[55vh] space-y-3 overflow-y-auto">
          {!selection && history.isPending && <p>{t(($) => $.tab_body.memory.loading)}</p>}
          {!selection && history.isError && <div role="alert" className="space-y-2"><p>{t(($) => $.tab_body.memory.load_failed)}</p><Button variant="outline" onClick={() => void history.refetch()}>{t(($) => $.tab_body.memory.retry_action)}</Button></div>}
          {!selection && !history.isPending && !history.isError && versions.length === 0 && <p>{t(($) => $.tab_body.memory.history_empty)}</p>}
          {versions.map((version) => (
            <div key={version.revision} className="space-y-2 rounded-lg border p-3 text-body">
              <p className="font-medium">{t(($) => $.tab_body.memory.version_label, { revision: version.revision })}</p>
              <p className="whitespace-pre-wrap break-words">{version.content}</p>
              <div className="flex flex-wrap gap-2 text-caption text-muted-foreground">
                {!(version.status === "active" && version.expired) && <Badge variant="outline">{statusLabel(version.status)}</Badge>}
                {version.expired && <Badge variant="secondary">{t(($) => $.tab_body.memory.expired)}</Badge>}
                {version.updated_at && <span>{new Date(version.updated_at).toLocaleString()}</span>}
              </div>
              <p className="text-caption text-muted-foreground">{version.expires_at ? t(($) => $.tab_body.memory.expires_on, { date: new Date(version.expires_at).toLocaleString() }) : t(($) => $.tab_body.memory.no_expiry)}</p>
              {version.restored_from_revision !== undefined && <p className="text-caption">{t(($) => $.tab_body.memory.restored_from, { revision: version.restored_from_revision })}</p>}
              {!selection && canRestore && version.revision !== memory.revision && ["active", "pending", "rejected"].includes(version.status ?? "") && <Button variant="outline" size="sm" onClick={() => { setError(null); setSelection({ version, expectedRevision: memory.revision! }); }}>{t(($) => $.tab_body.memory.restore_version, { revision: version.revision })}</Button>}
            </div>
          ))}
          {!selection && history.hasNextPage && <Button variant="outline" disabled={history.isFetchingNextPage} onClick={() => void history.fetchNextPage()}>{t(($) => $.tab_body.memory.older_versions)}</Button>}
          {error && <p role="alert" className="text-body text-destructive">{error}</p>}
        </div>
        <DialogFooter>
          <Button variant="ghost" disabled={restore.isPending} onClick={() => { if (selection) { setSelection(null); setError(null); } else onClose(); }}>{selection ? t(($) => $.tab_body.memory.back_history) : t(($) => $.tab_body.memory.dialog_cancel)}</Button>
          {selection && canRestore && <Button disabled={restore.isPending} onClick={() => {
            if (restore.isPending) return;
            setError(null);
            restore.mutate({ memoryId: memory.id, restore_revision: selection.version.revision, expected_revision: selection.expectedRevision }, {
              onSuccess: () => { setSelection(null); },
              onError: (err) => setError(err instanceof ApiError && err.status === 409 ? t(($) => $.tab_body.memory.conflict_toast) : t(($) => $.tab_body.memory.save_failed_toast)),
            });
          }}>{t(($) => $.tab_body.memory.restore_action)}</Button>}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
