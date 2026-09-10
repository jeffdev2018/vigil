"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Sparkles } from "lucide-react";
import { ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useDraftAutopilot, useProposeAutopilot } from "@multica/core/autopilots/mutations";
import type { AutopilotDraft, AutopilotExecutionMode } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { formatInTimeZone } from "../../common/format-in-time-zone";
import { useLocale, useT } from "../../i18n";

const DRAFT_TEXT_MAX = 2000;

/**
 * "From a sentence" (OS plan, vague B): write what should happen and when, the
 * model answers a title, a cron, a timezone and the agent's instruction, and
 * the next three firings prove it read the sentence right. Nothing is written
 * until the preview is accepted — POST /api/autopilots/draft writes nothing.
 *
 * "Create paused" files it paused behind a Decision Card someone answers;
 * "Create and activate" passes `activate: true` so the server creates it
 * active with its schedule enabled and no card — a person who is sure does not
 * have to answer their own question.
 *
 * 503 means no model is configured — the manual form is the way through, and
 * the notice says so rather than leaving a dead button.
 */
export function AutopilotDraftDialog({
  open,
  onOpenChange,
  onWriteYourself,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Opens the manual autopilot form — the way out when no model can draft. */
  onWriteYourself: () => void;
}) {
  const { t } = useT("autopilots");
  const locale = useLocale();
  const wsId = useWorkspaceId();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const draftAutopilot = useDraftAutopilot();
  const propose = useProposeAutopilot();

  const [text, setText] = useState("");
  const [draft, setDraft] = useState<AutopilotDraft | null>(null);
  const [nextRuns, setNextRuns] = useState<string[]>([]);
  const [assigneeId, setAssigneeId] = useState("");
  const [noModel, setNoModel] = useState(false);
  const [creating, setCreating] = useState(false);

  const activeAgents = agents.filter((a) => !a.archived_at);
  const fail = (e: unknown, fallback: string) =>
    toast.error(e instanceof Error && e.message ? e.message : fallback);

  const runDraft = () => {
    setNoModel(false);
    draftAutopilot.mutate(
      { text: text.trim(), timezone: Intl.DateTimeFormat().resolvedOptions().timeZone },
      {
        onSuccess: (d) => {
          setDraft(d);
          setNextRuns(d.next_runs);
        },
        onError: (e) => {
          if (e instanceof ApiError && e.status === 503) {
            setNoModel(true);
            return;
          }
          fail(e, t(($) => $.from_sentence.draft_failed));
        },
      },
    );
  };

  const patch = (over: Partial<AutopilotDraft>) =>
    setDraft((d) => (d ? { ...d, ...over } : d));

  const create = async (activate: boolean) => {
    if (!draft) return;
    setCreating(true);
    try {
      const res = await propose.mutateAsync({
        title: draft.title,
        cron_expression: draft.cron_expression,
        timezone: draft.timezone,
        description: draft.description,
        execution_mode: draft.execution_mode,
        issue_title_template: draft.issue_title_template || undefined,
        assignee_id: assigneeId,
        // One request: the server creates it active with its schedule enabled
        // and files no card. Members only — a run proposes, a person decides.
        activate,
      });
      setNextRuns(res.next_runs);
      toast.success(
        activate ? t(($) => $.from_sentence.created_active) : t(($) => $.from_sentence.created_paused),
      );
      onOpenChange(false);
    } catch (e) {
      fail(e, t(($) => $.from_sentence.create_failed));
    } finally {
      setCreating(false);
    }
  };

  const busy = creating || propose.isPending;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] max-w-lg overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Sparkles className="size-4" aria-hidden="true" />
            {t(($) => $.from_sentence.title)}
          </DialogTitle>
          <DialogDescription>{t(($) => $.from_sentence.description)}</DialogDescription>
        </DialogHeader>

        {noModel ? (
          <div role="alert" className="flex flex-col items-start gap-2 rounded-md border border-warning/60 bg-warning/5 p-2 text-caption">
            <p>{t(($) => $.from_sentence.no_model)}</p>
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => {
                onOpenChange(false);
                onWriteYourself();
              }}
            >
              {t(($) => $.from_sentence.write_yourself)}
            </Button>
          </div>
        ) : null}

        {draft === null ? (
          <div className="flex flex-col gap-2">
            <Textarea
              rows={4}
              maxLength={DRAFT_TEXT_MAX}
              aria-label={t(($) => $.from_sentence.sentence_label)}
              placeholder={t(($) => $.from_sentence.sentence_placeholder)}
              value={text}
              onChange={(e) => setText(e.target.value)}
            />
          </div>
        ) : (
          <div data-testid="autopilot-draft-preview" className="flex flex-col gap-3 text-caption">
            <p className="text-muted-foreground">{draft.reason}</p>

            <label className="flex flex-col gap-1">
              <span className="text-muted-foreground">{t(($) => $.from_sentence.field_title)}</span>
              <Input
                aria-label={t(($) => $.from_sentence.field_title)}
                value={draft.title}
                onChange={(e) => patch({ title: e.target.value })}
              />
            </label>

            <div className="flex gap-2">
              <label className="flex flex-1 flex-col gap-1">
                <span className="text-muted-foreground">{t(($) => $.from_sentence.field_cron)}</span>
                <Input
                  aria-label={t(($) => $.from_sentence.field_cron)}
                  className="font-mono"
                  value={draft.cron_expression}
                  onChange={(e) => patch({ cron_expression: e.target.value })}
                />
              </label>
              <label className="flex flex-1 flex-col gap-1">
                <span className="text-muted-foreground">{t(($) => $.from_sentence.field_timezone)}</span>
                <Input
                  aria-label={t(($) => $.from_sentence.field_timezone)}
                  value={draft.timezone}
                  onChange={(e) => patch({ timezone: e.target.value })}
                />
              </label>
            </div>

            <label className="flex flex-col gap-1">
              <span className="text-muted-foreground">{t(($) => $.from_sentence.field_prompt)}</span>
              <Textarea
                rows={3}
                aria-label={t(($) => $.from_sentence.field_prompt)}
                value={draft.description}
                onChange={(e) => patch({ description: e.target.value })}
              />
            </label>

            <label className="flex flex-col gap-1">
              <span className="text-muted-foreground">{t(($) => $.from_sentence.field_mode)}</span>
              <Select
                items={[
                  { value: "create_issue", label: t(($) => $.execution_mode.create_issue) },
                  { value: "run_only", label: t(($) => $.execution_mode.run_only) },
                ]}
                value={draft.execution_mode}
                onValueChange={(v) => v && patch({ execution_mode: v as AutopilotExecutionMode })}
              >
                <SelectTrigger aria-label={t(($) => $.from_sentence.field_mode)}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="create_issue">{t(($) => $.execution_mode.create_issue)}</SelectItem>
                  <SelectItem value="run_only">{t(($) => $.execution_mode.run_only)}</SelectItem>
                </SelectContent>
              </Select>
            </label>

            {draft.execution_mode === "create_issue" && (
              <label className="flex flex-col gap-1">
                <span className="text-muted-foreground">{t(($) => $.from_sentence.field_issue_title)}</span>
                <Input
                  aria-label={t(($) => $.from_sentence.field_issue_title)}
                  value={draft.issue_title_template ?? ""}
                  onChange={(e) => patch({ issue_title_template: e.target.value })}
                />
              </label>
            )}

            <label className="flex flex-col gap-1">
              <span className="text-muted-foreground">{t(($) => $.from_sentence.field_assignee)}</span>
              <Select
                items={activeAgents.map((a) => ({ value: a.id, label: a.name }))}
                value={assigneeId}
                onValueChange={(v) => v && setAssigneeId(v)}
              >
                <SelectTrigger aria-label={t(($) => $.from_sentence.field_assignee)}>
                  <SelectValue placeholder={t(($) => $.from_sentence.assignee_placeholder)} />
                </SelectTrigger>
                <SelectContent>
                  {activeAgents.map((a) => (
                    <SelectItem key={a.id} value={a.id}>
                      {a.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </label>

            {nextRuns.length > 0 && (
              <div className="flex flex-col gap-0.5">
                <span className="text-muted-foreground">{t(($) => $.from_sentence.next_runs)}</span>
                <ul className="flex flex-col tabular-nums">
                  {nextRuns.map((at) => (
                    <li key={at}>{formatInTimeZone(at, draft.timezone, locale)}</li>
                  ))}
                </ul>
              </div>
            )}
          </div>
        )}

        <DialogFooter>
          {draft === null ? (
            <Button
              type="button"
              disabled={text.trim() === "" || draftAutopilot.isPending}
              onClick={runDraft}
            >
              {t(($) => $.from_sentence.draft)}
            </Button>
          ) : (
            <>
              <Button type="button" variant="ghost" onClick={() => setDraft(null)}>
                {t(($) => $.from_sentence.back)}
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={assigneeId === "" || busy}
                onClick={() => void create(false)}
              >
                {t(($) => $.from_sentence.create_paused)}
              </Button>
              <Button
                type="button"
                disabled={assigneeId === "" || busy}
                onClick={() => void create(true)}
              >
                {t(($) => $.from_sentence.create_active)}
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
