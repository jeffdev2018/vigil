"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Brain, Info, MoreHorizontal, Pencil, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import type { Agent, AgentMemory } from "@multica/core/types";
import { ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  agentMemoryOptions,
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
import { AppLink } from "../../../navigation";
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
  const wsPaths = useWorkspacePaths();
  const { data, isLoading } = useQuery(agentMemoryOptions(wsId, agent.id));
  const memories = data?.memories ?? [];
  // The server owns the brief character budget, so trust its count rather
  // than re-deriving one here; only flag it when it actually truncated.
  const briefedCount = data?.briefed_count ?? memories.length;
  const truncated = briefedCount < memories.length;
  const extractionDisabled = data?.extraction_enabled === false;

  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<AgentMemory | null>(null);
  const [pendingDelete, setPendingDelete] = useState<AgentMemory | null>(null);
  const approve = useUpdateAgentMemory(agent.id);
  const [approvingId, setApprovingId] = useState<string | null>(null);

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
      <p className="text-body leading-6 text-muted-foreground">
        {t(($) => $.tab_body.memory.intro)}
      </p>

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
            {truncated && (
              <p className="mt-1 text-caption leading-5 text-muted-foreground">
                {t(($) => $.tab_body.memory.briefed_note, {
                  count: briefedCount,
                  total: memories.length,
                })}
              </p>
            )}
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
            {memories.map((memory) => (
              <li key={memory.id} className="flex items-start gap-3 p-3">
                <span className="min-w-0 flex-1">
                  <span className="block whitespace-pre-wrap break-words text-body">
                    {memory.content}
                  </span>
                  <span className="mt-1.5 flex items-center gap-2 text-caption text-muted-foreground">
                    <Badge
                      variant={memory.source === "manual" ? "outline" : "secondary"}
                    >
                      {memory.source === "run"
                        ? t(($) => $.tab_body.memory.source_run)
                        : memory.source === "postmortem"
                          ? t(($) => $.tab_body.memory.source_postmortem)
                          : t(($) => $.tab_body.memory.source_manual)}
                    </Badge>
                    {memory.state === "draft" && (
                      <Badge variant="outline" className="border-dashed">
                        {t(($) => $.tab_body.memory.state_draft)}
                      </Badge>
                    )}
                    {memory.updated_at && <span>{timeAgo(memory.updated_at)}</span>}
                    {memory.source_issue_id && (
                      <AppLink
                        href={wsPaths.issueDetail(memory.source_issue_id)}
                        className="text-brand hover:underline"
                      >
                        {t(($) => $.tab_body.memory.source_issue_link)}
                      </AppLink>
                    )}
                  </span>
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

      {extractionDisabled && (
        <p className="flex items-start gap-2 text-caption leading-5 text-muted-foreground">
          <Info className="mt-0.5 size-3.5 shrink-0" aria-hidden />
          <span>{t(($) => $.tab_body.memory.extraction_disabled_note)}</span>
        </p>
      )}

      <MemoryEditorDialog
        agentId={agent.id}
        open={editorOpen}
        onOpenChange={setEditorOpen}
      />
      <MemoryEditorDialog
        agentId={agent.id}
        open={Boolean(editing)}
        onOpenChange={(open) => !open && setEditing(null)}
        memory={editing}
      />
      <DeleteMemoryDialog
        agentId={agent.id}
        memory={pendingDelete}
        onClose={() => setPendingDelete(null)}
      />
    </div>
  );
}

function MemoryEditorDialog({
  agentId,
  open,
  onOpenChange,
  memory,
}: {
  agentId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  memory?: AgentMemory | null;
}) {
  const { t } = useT("agents");
  const create = useCreateAgentMemory(agentId);
  const update = useUpdateAgentMemory(agentId);
  const [content, setContent] = useState("");

  useEffect(() => {
    if (!open) return;
    setContent(memory?.content ?? "");
  }, [memory, open]);

  const trimmed = content.trim();
  const pending = create.isPending || update.isPending;

  const handleError = (error: unknown, fallback: string) => {
    if (error instanceof ApiError && error.status === 409) {
      toast.error(t(($) => $.tab_body.memory.cap_reached_toast));
      return;
    }
    toast.error(error instanceof Error ? error.message : fallback);
  };

  const submit = () => {
    if (!trimmed) return;
    if (memory) {
      update.mutate(
        { memoryId: memory.id, content: trimmed },
        {
          onSuccess: () => onOpenChange(false),
          onError: (error) =>
            handleError(error, t(($) => $.tab_body.memory.save_failed_toast)),
        },
      );
      return;
    }
    create.mutate(trimmed, {
      onSuccess: () => onOpenChange(false),
      onError: (error) =>
        handleError(error, t(($) => $.tab_body.memory.save_failed_toast)),
    });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {memory
              ? t(($) => $.tab_body.memory.dialog_edit_title)
              : t(($) => $.tab_body.memory.dialog_create_title)}
          </DialogTitle>
          <DialogDescription>
            {t(($) => $.tab_body.memory.dialog_description)}
          </DialogDescription>
        </DialogHeader>
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
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t(($) => $.tab_body.memory.dialog_cancel)}
          </Button>
          <Button onClick={submit} disabled={!trimmed || pending}>
            {pending
              ? t(($) => $.tab_body.memory.dialog_saving)
              : t(($) => $.tab_body.memory.dialog_save)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function DeleteMemoryDialog({
  agentId,
  memory,
  onClose,
}: {
  agentId: string;
  memory: AgentMemory | null;
  onClose: () => void;
}) {
  const { t } = useT("agents");
  const remove = useDeleteAgentMemory(agentId);
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
