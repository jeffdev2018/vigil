"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { chatKeys } from "@multica/core/chat/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueKeys } from "@multica/core/issues/queries";
import { useCreateIssueDecision } from "@multica/core/projects/decisions";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { useT } from "../../i18n";

/**
 * Write an architecture decision by hand (K29).
 *
 * Until now only the LLM extractor created ADRs — a human could read decision
 * memory but never add to it. The shape of this dialog is dictated by
 * `POST /api/issues/{id}/decision-records`, not by preference:
 *
 *   * The record hangs off an ISSUE, so the project's issues are the first
 *     field. A project-level decision has nowhere to live server-side.
 *   * Every record must cite a message of a run — the endpoint refuses a seq
 *     it cannot find with 422 `invalid_source` — so the cited message is a
 *     field, not a hidden default.
 *   * `run_id` is optional server-side and falls back to the issue's last
 *     COMPLETED run. We pick that same run client-side to list its messages,
 *     and then send its id explicitly: relying on the default would let a run
 *     finishing mid-dialog rebind the seqs the user was shown.
 *   * `title` and `decision` are required; `context` and `consequences` are
 *     optional and stored trimmed.
 *
 * An issue with no completed run cannot carry a record at all. Rather than
 * fail on submit, such an issue is not offered.
 */
export function RecordDecisionDialog({
  projectId,
  open,
  onOpenChange,
}: {
  projectId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const create = useCreateIssueDecision(wsId);

  const [issueId, setIssueId] = useState("");
  const [seq, setSeq] = useState("");
  const [title, setTitle] = useState("");
  const [context, setContext] = useState("");
  const [decision, setDecision] = useState("");
  const [consequences, setConsequences] = useState("");

  // Reset on open so a cancelled draft never leaks into the next one.
  useEffect(() => {
    if (!open) return;
    setIssueId("");
    setSeq("");
    setTitle("");
    setContext("");
    setDecision("");
    setConsequences("");
  }, [open]);

  const { data: issueList } = useQuery({
    queryKey: ["decisions", "record-dialog", "issues", wsId, projectId],
    queryFn: () => api.listIssues({ project_id: projectId, limit: 100 }),
    enabled: open && !!projectId,
  });
  const issues = issueList?.issues ?? [];

  // The chosen issue's runs. Same cache the issue detail's execution log uses.
  const { data: tasks = [], isLoading: tasksLoading } = useQuery({
    queryKey: issueKeys.tasks(issueId),
    queryFn: () => api.listTasksByIssue(issueId),
    enabled: open && !!issueId,
  });

  // Mirrors the server's GetLatestCompletedTaskForIssue: completed only,
  // newest by completed_at, created_at breaking a tie.
  const run = useMemo(() => {
    const completed = tasks.filter((task) => task.status === "completed");
    return (
      [...completed].sort((a, b) => {
        const at = Date.parse(a.completed_at ?? "") || 0;
        const bt = Date.parse(b.completed_at ?? "") || 0;
        return bt - at || Date.parse(b.created_at) - Date.parse(a.created_at);
      })[0] ?? null
    );
  }, [tasks]);

  const { data: messages = [], isLoading: messagesLoading } = useQuery({
    queryKey: chatKeys.taskMessages(run?.id ?? ""),
    queryFn: () => api.listTaskMessages(run!.id),
    enabled: open && !!run?.id,
  });

  // The endpoint accepts any seq present in the run; `action` rows are
  // synthesized issue changes rather than stored messages, so citing one
  // would be refused. Filter them out instead of letting the user find out.
  const citable = useMemo(
    () => messages.filter((m) => m.type !== "action"),
    [messages],
  );

  useEffect(() => {
    setSeq("");
  }, [issueId]);

  const loadingRun = !!issueId && (tasksLoading || (!!run && messagesLoading));
  const noRun = !!issueId && !tasksLoading && !run;
  const canSubmit =
    !!issueId &&
    !!run &&
    seq !== "" &&
    title.trim().length > 0 &&
    decision.trim().length > 0 &&
    !create.isPending;

  const handleSubmit = () => {
    if (!canSubmit || !run) return;
    create.mutate(
      {
        issueId,
        runId: run.id,
        decision: {
          source_message_seq: Number(seq),
          title: title.trim(),
          decision: decision.trim(),
          context: context.trim(),
          consequences: consequences.trim(),
        },
      },
      {
        onSuccess: () => {
          toast.success(t(($) => $.decisions.record.toast_saved));
          onOpenChange(false);
        },
        onError: (err) =>
          toast.error(
            err instanceof Error && err.message
              ? err.message
              : t(($) => $.decisions.record.toast_failed),
          ),
      },
    );
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (create.isPending) return;
        onOpenChange(next);
      }}
    >
      <DialogContent className="w-[calc(100vw-2rem)] !max-w-[520px] gap-0 overflow-hidden rounded-lg p-0">
        <div className="max-h-[70vh] overflow-y-auto px-5 pb-4 pt-5">
          <DialogTitle className="text-title-sm font-semibold">
            {t(($) => $.decisions.record.title)}
          </DialogTitle>
          <p className="mt-1 text-body leading-5 text-muted-foreground">
            {t(($) => $.decisions.record.description)}
          </p>

          <div className="mt-4 flex flex-col gap-3">
            <Field label={t(($) => $.decisions.record.issue_label)}>
              <Select
                items={[
                  { value: "", label: t(($) => $.decisions.record.issue_placeholder) },
                  ...issues.map((issue) => ({
                    value: issue.id,
                    label: issue.identifier
                      ? `${issue.identifier} ${issue.title}`
                      : issue.title,
                  })),
                ]}
                value={issueId}
                onValueChange={(value) => value !== null && setIssueId(value)}
              >
                <SelectTrigger className="w-full" aria-label={t(($) => $.decisions.record.issue_label)}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="">
                    {t(($) => $.decisions.record.issue_placeholder)}
                  </SelectItem>
                  {issues.map((issue) => (
                    <SelectItem key={issue.id} value={issue.id}>
                      {issue.identifier
                        ? `${issue.identifier} ${issue.title}`
                        : issue.title}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>

            <Field label={t(($) => $.decisions.record.source_label)}>
              <Select
                items={[
                  { value: "", label: t(($) => $.decisions.record.source_placeholder) },
                  ...citable.map((message) => ({
                    value: String(message.seq),
                    label: `#${message.seq} · ${previewOf(message.content ?? message.output ?? "")}`,
                  })),
                ]}
                value={seq}
                onValueChange={(value) => value !== null && setSeq(value)}
              >
                <SelectTrigger
                  className="w-full"
                  aria-label={t(($) => $.decisions.record.source_label)}
                  disabled={!run || citable.length === 0}
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="">
                    {t(($) => $.decisions.record.source_placeholder)}
                  </SelectItem>
                  {citable.map((message) => (
                    <SelectItem key={message.seq} value={String(message.seq)}>
                      {`#${message.seq} · ${previewOf(message.content ?? message.output ?? "")}`}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {loadingRun && (
                <p className="mt-1 text-caption text-muted-foreground">
                  {t(($) => $.decisions.record.source_loading)}
                </p>
              )}
              {noRun && (
                <p
                  data-testid="record-decision-no-run"
                  className="mt-1 text-caption text-muted-foreground"
                >
                  {t(($) => $.decisions.record.no_run)}
                </p>
              )}
            </Field>

            <Field label={t(($) => $.decisions.record.title_label)}>
              <Input
                value={title}
                maxLength={200}
                aria-label={t(($) => $.decisions.record.title_label)}
                onChange={(e) => setTitle(e.target.value)}
              />
            </Field>

            <Field label={t(($) => $.decisions.record.context_label)}>
              <Textarea
                rows={2}
                value={context}
                aria-label={t(($) => $.decisions.record.context_label)}
                onChange={(e) => setContext(e.target.value)}
              />
            </Field>

            <Field label={t(($) => $.decisions.record.decision_label)}>
              <Textarea
                rows={3}
                value={decision}
                aria-label={t(($) => $.decisions.record.decision_label)}
                onChange={(e) => setDecision(e.target.value)}
              />
            </Field>

            <Field label={t(($) => $.decisions.record.consequences_label)}>
              <Textarea
                rows={2}
                value={consequences}
                aria-label={t(($) => $.decisions.record.consequences_label)}
                onChange={(e) => setConsequences(e.target.value)}
              />
            </Field>
          </div>
        </div>

        <div className="border-t bg-muted/25 px-5 py-3">
          <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
            <Button
              type="button"
              variant="outline"
              className="w-full sm:w-auto"
              onClick={() => onOpenChange(false)}
              disabled={create.isPending}
            >
              {t(($) => $.decisions.record.cancel)}
            </Button>
            <Button
              type="button"
              className="w-full sm:w-auto"
              onClick={handleSubmit}
              disabled={!canSubmit}
            >
              {create.isPending
                ? t(($) => $.decisions.record.saving)
                : t(($) => $.decisions.record.save)}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

/** One-line preview of a run message, so the seq picker is readable. */
function previewOf(content: string): string {
  const flat = content.replace(/\s+/g, " ").trim();
  return flat.length > 60 ? `${flat.slice(0, 60)}…` : flat;
}

function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-caption font-medium text-muted-foreground">
        {label}
      </span>
      {children}
    </label>
  );
}
