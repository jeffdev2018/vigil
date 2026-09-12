"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { AlarmClock, Trash2 } from "lucide-react";
import { ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { formatCountdown } from "@multica/core/approvals";
import {
  FOLLOWUP_NOTE_MAX,
  FOLLOWUP_QUICK_CHOICES,
  followupQuickChoiceInstant,
  followupSecondsUntil,
  issueFollowupsOptions,
  useCancelFollowup,
  useScheduleFollowup,
  type FollowupQuickChoice,
} from "@multica/core/followups";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import type { Followup, Issue } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
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
import { useLocale, useT } from "../../i18n";

/**
 * Follow-ups (OS plan, vague B, "réveil programmé"): a deferred wake-up of
 * this issue's agent — "come back at 9 tomorrow and check whether staging is
 * green". The same rows show in the runs fleet (blocked on "deferred") and in
 * the calendar agenda; this block is where a person files and cancels one.
 *
 * Nothing is optimistic: the daily budget can refuse a wake-up (429) and the
 * scheduler can win the race with a cancel (409), so both await the server —
 * see packages/core/followups/mutations.ts.
 */
export function FollowupsSection({
  issueId,
  issue,
}: {
  issueId: string;
  issue: Pick<Issue, "assignee_type" | "assignee_id">;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data, isPending } = useQuery(issueFollowupsOptions(wsId, issueId));
  const cancel = useCancelFollowup(wsId, issueId);
  const [scheduling, setScheduling] = useState(false);
  const [pendingCancel, setPendingCancel] = useState<Followup | null>(null);

  if (isPending) return null;

  const followups = data?.followups ?? [];
  const budget = data?.budget;

  return (
    <div data-testid="followups-section" className="flex flex-col gap-2 rounded-md border p-2 text-caption">
      <div className="flex items-center gap-2">
        <AlarmClock className="size-3.5 text-muted-foreground" aria-hidden="true" />
        <span className="font-medium">{t(($) => $.followups.section)}</span>
        <Button
          type="button"
          size="sm"
          variant="ghost"
          className="ml-auto"
          onClick={() => setScheduling(true)}
        >
          {t(($) => $.followups.schedule)}
        </Button>
      </div>

      {followups.length === 0 ? (
        <p className="text-muted-foreground">{t(($) => $.followups.empty)}</p>
      ) : (
        <ul className="flex flex-col gap-1.5">
          {followups.map((f) => (
            <FollowupRow key={f.id} followup={f} onCancel={() => setPendingCancel(f)} />
          ))}
        </ul>
      )}

      {budget && budget.max_per_agent_per_day > 0 && (
        <p className="text-muted-foreground">
          {t(($) => $.followups.budget_line, {
            agent: budget.max_per_agent_per_day,
            workspace: budget.max_per_workspace_per_day,
          })}
        </p>
      )}

      {scheduling && (
        <ScheduleFollowupDialog
          issueId={issueId}
          issue={issue}
          budgetPerAgent={budget?.max_per_agent_per_day ?? 0}
          onClose={() => setScheduling(false)}
        />
      )}

      {/* Cancel awaits the server: the scheduler may already have fired the
          wake-up (409), and a row removed optimistically would then reappear. */}
      <AlertDialog open={pendingCancel !== null} onOpenChange={(open) => !open && setPendingCancel(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.followups.cancel_dialog.title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.followups.cancel_dialog.description, {
                agent: pendingCancel?.agent_name ?? "",
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.followups.cancel_dialog.keep)}</AlertDialogCancel>
            <AlertDialogAction
              disabled={cancel.isPending}
              onClick={() => {
                const target = pendingCancel;
                if (!target) return;
                cancel.mutate(target.id, {
                  onSuccess: () => {
                    setPendingCancel(null);
                    toast.success(t(($) => $.followups.cancelled));
                  },
                  onError: (e) =>
                    toast.error(
                      e instanceof Error && e.message ? e.message : t(($) => $.followups.cancel_failed),
                    ),
                });
              }}
            >
              {t(($) => $.followups.cancel_dialog.confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

/** One pending wake-up: when, who wakes up, the note, and who filed it. */
function FollowupRow({ followup, onCancel }: { followup: Followup; onCancel: () => void }) {
  const { t } = useT("issues");
  const locale = useLocale();
  const { getActorName } = useActorName();
  const scheduledByName = followup.scheduled_by_id
    ? getActorName(followup.scheduled_by_type === "agent" ? "agent" : "member", followup.scheduled_by_id)
    : "";
  const absolute = useMemo(() => {
    const at = new Date(followup.fires_at);
    return Number.isNaN(at.getTime()) ? followup.fires_at : at.toLocaleString(locale);
  }, [followup.fires_at, locale]);
  const countdown = formatCountdown(followupSecondsUntil(followup.fires_at));

  return (
    <li data-testid="followup-row" className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
      {/* Relative reads at a glance; the exact instant is one hover away. */}
      <span className="font-medium tabular-nums" title={absolute}>
        {countdown ? t(($) => $.followups.fires_in, { countdown }) : absolute}
      </span>
      <span className="text-muted-foreground">{followup.agent_name}</span>
      <span className="min-w-0 flex-1 truncate">{followup.note}</span>
      <span className="text-muted-foreground">
        {t(($) => $.followups.scheduled_by, { by: scheduledByName || followup.scheduled_by_type })}
      </span>
      <Button
        type="button"
        size="icon-sm"
        variant="ghost"
        className="text-muted-foreground hover:text-destructive"
        aria-label={t(($) => $.followups.cancel)}
        onClick={onCancel}
      >
        <Trash2 className="size-3.5" />
      </Button>
    </li>
  );
}

/**
 * When should the agent come back? Three quick answers plus a custom instant,
 * a note, and — when the issue has no agent assignee — which agent wakes up.
 * The datetime input carries no timezone, so it is read as the browser's
 * wall clock and resolved to an instant before it leaves (RFC 3339 on the wire).
 */
function ScheduleFollowupDialog({
  issueId,
  issue,
  budgetPerAgent,
  onClose,
}: {
  issueId: string;
  issue: Pick<Issue, "assignee_type" | "assignee_id">;
  budgetPerAgent: number;
  onClose: () => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const schedule = useScheduleFollowup(wsId, issueId);
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const needsAgent = issue.assignee_type !== "agent" || !issue.assignee_id;
  const activeAgents = useMemo(() => agents.filter((a) => !a.archived_at), [agents]);

  const [choice, setChoice] = useState<FollowupQuickChoice | "custom">("in_1h");
  const [custom, setCustom] = useState("");
  const [note, setNote] = useState("");
  const [agentId, setAgentId] = useState("");
  // The budget refusal is the one error that belongs in the form rather than a
  // toast: it explains why this submit did nothing and what the cap is.
  const [refusal, setRefusal] = useState("");

  const when =
    choice === "custom"
      ? custom
        ? new Date(custom).toISOString()
        : ""
      : followupQuickChoiceInstant(choice);
  const canSubmit = when !== "" && (!needsAgent || agentId !== "") && !schedule.isPending;

  const submit = () => {
    setRefusal("");
    schedule.mutate(
      {
        when,
        note: note.trim() || undefined,
        agent_id: needsAgent ? agentId : undefined,
      },
      {
        onSuccess: () => {
          toast.success(t(($) => $.followups.scheduled));
          onClose();
        },
        onError: (e) => {
          if (e instanceof ApiError && e.status === 429) {
            setRefusal(e.message || t(($) => $.followups.budget_reached));
            return;
          }
          toast.error(e instanceof Error && e.message ? e.message : t(($) => $.followups.schedule_failed));
        },
      },
    );
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.followups.dialog.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.followups.dialog.description)}</DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-3 text-caption">
          <div className="flex flex-wrap gap-1.5">
            {FOLLOWUP_QUICK_CHOICES.map((c) => (
              <Button
                key={c}
                type="button"
                size="sm"
                // The chosen one must stay readable while hovered, so the
                // selected state carries weight and tone, not just a fill.
                variant={choice === c ? "default" : "outline"}
                className={choice === c ? "font-medium" : undefined}
                aria-pressed={choice === c}
                onClick={() => setChoice(c)}
              >
                {t(($) => $.followups.quick[c])}
              </Button>
            ))}
            <Button
              type="button"
              size="sm"
              variant={choice === "custom" ? "default" : "outline"}
              className={choice === "custom" ? "font-medium" : undefined}
              aria-pressed={choice === "custom"}
              onClick={() => setChoice("custom")}
            >
              {t(($) => $.followups.quick.custom)}
            </Button>
          </div>

          {choice === "custom" && (
            <Input
              type="datetime-local"
              aria-label={t(($) => $.followups.dialog.custom_label)}
              value={custom}
              onChange={(e) => setCustom(e.target.value)}
            />
          )}

          {needsAgent && (
            <label className="flex flex-col gap-1">
              <span className="text-muted-foreground">{t(($) => $.followups.dialog.agent_label)}</span>
              <Select
                items={activeAgents.map((a) => ({ value: a.id, label: a.name }))}
                value={agentId}
                onValueChange={(v) => v && setAgentId(v)}
              >
                <SelectTrigger aria-label={t(($) => $.followups.dialog.agent_label)}>
                  <SelectValue placeholder={t(($) => $.followups.dialog.agent_placeholder)} />
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
          )}

          <label className="flex flex-col gap-1">
            <span className="text-muted-foreground">{t(($) => $.followups.dialog.note_label)}</span>
            <Input
              maxLength={FOLLOWUP_NOTE_MAX}
              aria-label={t(($) => $.followups.dialog.note_label)}
              placeholder={t(($) => $.followups.dialog.note_placeholder)}
              value={note}
              onChange={(e) => setNote(e.target.value)}
            />
          </label>

          {budgetPerAgent > 0 && (
            <p className="text-muted-foreground">
              {t(($) => $.followups.dialog.budget_hint, { agent: budgetPerAgent })}
            </p>
          )}

          {refusal && (
            <p role="alert" className="text-destructive">
              {refusal}
            </p>
          )}
        </div>

        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>
            {t(($) => $.followups.dialog.cancel)}
          </Button>
          <Button type="button" disabled={!canSubmit} onClick={submit}>
            {t(($) => $.followups.dialog.confirm)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
