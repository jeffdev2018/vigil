"use client";

import { useCallback, useMemo, useState } from "react";
import { toast } from "sonner";
import { Check, ChevronDown, Clock, FolderKanban, GitMerge, Loader2, X } from "lucide-react";
import { ApiError } from "@multica/core/api";
import { useActorName } from "@multica/core/workspace/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import type {
  AcceptTriageItemOverrides,
  IssueAssigneeType,
  IssuePriority,
  TriageItem,
} from "@multica/core/types";
import {
  useAcceptTriageItem,
  useDismissTriageItem,
  useMergeTriageItem,
  useSnoozeTriageItem,
} from "@multica/core/triage/mutations";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { cn } from "@multica/ui/lib/utils";
import { AssigneePicker, PriorityPicker } from "../../issues/components/pickers";
import { PriorityIcon } from "../../issues/components/priority-icon";
import { ProjectPicker } from "../../projects/components/project-picker";
import { IssuePickerModal } from "../../modals/issue-picker-modal";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { SNOOZE_PRESETS, customSnoozeIso, resolveSnoozePreset } from "./snooze-presets";
import { useShortcutAction } from "./use-shortcut-action";

/**
 * The item's agent suggestion, or null. Loose on purpose: a verdict value
 * added server-side must render as "no suggestion", never as a crash.
 */
function agentVerdict(item: TriageItem): "accept" | "dismiss" | null {
  if (item.verdict === "accept") return "accept";
  if (item.verdict === "dismiss") return "dismiss";
  return null;
}

/**
 * "Agent suggests: Accept/Dismiss". Advisory only — the item is still
 * pending, and the matching human button is emphasized next to it.
 */
export function TriageVerdictBadge({
  item,
  className,
}: {
  item: TriageItem;
  className?: string;
}) {
  const { t } = useT("triage");
  const verdict = agentVerdict(item);
  if (!verdict) return null;
  return (
    <Badge
      variant="secondary"
      className={cn("shrink-0 gap-1 font-normal", className)}
      data-testid="triage-verdict-badge"
    >
      {verdict === "accept" ? (
        <Check aria-hidden="true" className="size-3" />
      ) : (
        <X aria-hidden="true" className="size-3" />
      )}
      {t(($) => $.verdict.badge, {
        verdict: t(($) => $.verdict[verdict]),
      })}
    </Badge>
  );
}

/** The verdict's reason and who suggested it, for the detail pane. */
export function TriageVerdictNote({ item }: { item: TriageItem }) {
  const { t } = useT("triage");
  const { getAgentName } = useActorName();
  const verdict = agentVerdict(item);
  if (!verdict) return null;
  return (
    <p
      data-testid="triage-verdict-note"
      className="shrink-0 border-b px-4 py-2 text-caption text-muted-foreground"
    >
      <span className="font-medium text-foreground">
        {t(($) => $.verdict.badge, { verdict: t(($) => $.verdict[verdict]) })}
      </span>
      {item.verdict_agent_id
        ? ` · ${t(($) => $.verdict.by, { name: getAgentName(item.verdict_agent_id) })}`
        : ""}
      {item.verdict_reason ? ` — ${item.verdict_reason}` : ""}
    </p>
  );
}

/**
 * Everything a human can do to a pending item: accept it (optionally as
 * somebody / somewhere / at some priority), merge it into an issue that
 * already tracks the work, park it with a snooze, or dismiss it with a
 * reason. None of these is optimistic — each one changes server state and the
 * caller awaits the result.
 */
export function TriageItemActions({
  item,
  wsId,
  onResolved,
}: {
  item: TriageItem;
  wsId: string;
  onResolved: () => void;
}) {
  const { t } = useT("triage");
  const navigation = useNavigation();
  const wsPaths = useWorkspacePaths();
  const accept = useAcceptTriageItem(wsId);
  const dismiss = useDismissTriageItem(wsId);
  const merge = useMergeTriageItem(wsId);
  const snooze = useSnoozeTriageItem(wsId);

  const [assigneeType, setAssigneeType] = useState<IssueAssigneeType | null>(null);
  const [assigneeId, setAssigneeId] = useState<string | null>(null);
  const [priority, setPriority] = useState<IssuePriority>("none");
  const [projectId, setProjectId] = useState<string | null>(null);
  const [dismissReason, setDismissReason] = useState("");
  const [dismissOpen, setDismissOpen] = useState(false);
  const [mergeOpen, setMergeOpen] = useState(false);
  const [customSnooze, setCustomSnooze] = useState("");
  const [snoozeOpen, setSnoozeOpen] = useState(false);

  const busy =
    accept.isPending || dismiss.isPending || merge.isPending || snooze.isPending;

  const overrides = useMemo<AcceptTriageItemOverrides>(() => {
    const next: AcceptTriageItemOverrides = {};
    if (assigneeType && assigneeId) {
      next.assignee_type = assigneeType === "agent" ? "agent" : "member";
      next.assignee_id = assigneeId;
    }
    if (projectId) next.project_id = projectId;
    if (priority !== "none") next.priority = priority;
    return next;
  }, [assigneeType, assigneeId, projectId, priority]);

  const handleAccept = useCallback(async () => {
    try {
      const res = await accept.mutateAsync({ itemId: item.id, overrides });
      const issue = res.issue;
      if (issue?.id && issue.identifier) {
        toast.success(
          t(($) => $.detail.accepted_toast_identifier, { identifier: issue.identifier }),
          {
            action: {
              label: t(($) => $.detail.open_issue),
              onClick: () => navigation.push(wsPaths.issueDetail(issue.id)),
            },
          },
        );
      } else {
        toast.success(t(($) => $.detail.accepted_toast));
      }
      onResolved();
    } catch (err) {
      handleTriageAcceptError(err, t);
    }
  }, [accept, item.id, navigation, onResolved, overrides, t, wsPaths]);

  const handleDismiss = useCallback(async () => {
    try {
      await dismiss.mutateAsync({
        itemId: item.id,
        reason: dismissReason.trim() || undefined,
      });
      toast.success(t(($) => $.detail.dismissed_toast));
      setDismissOpen(false);
      onResolved();
    } catch {
      toast.error(t(($) => $.detail.error_toast));
    }
  }, [dismiss, dismissReason, item.id, onResolved, t]);

  const handleSnooze = useCallback(
    async (until: Date) => {
      try {
        await snooze.mutateAsync({ itemId: item.id, until: until.toISOString() });
        toast.success(t(($) => $.snooze.toast));
        setSnoozeOpen(false);
        onResolved();
      } catch {
        toast.error(t(($) => $.detail.error_toast));
      }
    },
    [item.id, onResolved, snooze, t],
  );

  // Keyboard: the four verbs of an open item. Accept and dismiss run; snooze
  // and merge open their own pickers, because neither has a sensible default.
  // Every binding is inert while a mutation is in flight, so a double tap
  // cannot fire the same accept twice.
  useShortcutAction("triageAccept", busy ? null : () => void handleAccept());
  useShortcutAction("triageDismiss", busy ? null : () => void handleDismiss());
  useShortcutAction("triageSnooze", busy ? null : () => setSnoozeOpen(true));
  useShortcutAction("triageMerge", busy ? null : () => setMergeOpen(true));

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-1.5">
        <span className="text-caption text-muted-foreground">
          {t(($) => $.accept_as.label)}
        </span>
        <AssigneePicker
          assigneeType={assigneeType}
          assigneeId={assigneeId}
          onUpdate={(updates) => {
            setAssigneeType((updates.assignee_type ?? null) as IssueAssigneeType | null);
            setAssigneeId(updates.assignee_id ?? null);
          }}
          align="start"
          trigger={
            <span className="text-caption">
              {assigneeId
                ? t(($) => $.accept_as.assignee_set)
                : t(($) => $.accept_as.assignee)}
            </span>
          }
        />
        <PriorityPicker
          priority={priority}
          onUpdate={(updates) => setPriority((updates.priority ?? "none") as IssuePriority)}
          align="start"
          trigger={
            <span className="flex items-center gap-1 text-caption">
              <PriorityIcon priority={priority} className="size-3.5" />
              {t(($) => $.accept_as.priority)}
            </span>
          }
        />
        <ProjectPicker
          projectId={projectId}
          onUpdate={(updates) => setProjectId(updates.project_id ?? null)}
          align="start"
          triggerRender={
            <button
              type="button"
              className="inline-flex items-center gap-1 rounded-md border px-2 py-1 text-caption transition-colors hover:bg-accent/60"
            >
              <FolderKanban aria-hidden="true" className="size-3.5" />
              {projectId
                ? t(($) => $.accept_as.project_set)
                : t(($) => $.accept_as.project)}
              <ChevronDown aria-hidden="true" className="size-3 text-muted-foreground" />
            </button>
          }
        />
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <Popover open={dismissOpen} onOpenChange={setDismissOpen}>
          <PopoverTrigger
            render={
              <Button variant="outline" size="sm" disabled={busy}>
                <X aria-hidden="true" className="size-3.5" />
                {t(($) => $.detail.dismiss)}
              </Button>
            }
          />
          <PopoverContent align="end" className="w-72">
            <div className="flex flex-col gap-2">
              <label className="text-caption text-muted-foreground" htmlFor="triage-dismiss-reason">
                {t(($) => $.dismiss_reason.label)}
              </label>
              <Input
                id="triage-dismiss-reason"
                value={dismissReason}
                placeholder={t(($) => $.dismiss_reason.placeholder)}
                onChange={(e) => setDismissReason(e.target.value)}
              />
              <Button size="sm" onClick={handleDismiss} disabled={dismiss.isPending}>
                {dismiss.isPending ? (
                  <Loader2 aria-hidden="true" className="size-3.5 animate-spin" />
                ) : null}
                {dismiss.isPending
                  ? t(($) => $.detail.dismissing)
                  : t(($) => $.dismiss_reason.confirm)}
              </Button>
            </div>
          </PopoverContent>
        </Popover>

        <Button variant="outline" size="sm" disabled={busy} onClick={() => setMergeOpen(true)}>
          <GitMerge aria-hidden="true" className="size-3.5" />
          {t(($) => $.merge.action)}
        </Button>

        <DropdownMenu open={snoozeOpen} onOpenChange={setSnoozeOpen}>
          <DropdownMenuTrigger
            render={
              <Button variant="outline" size="sm" disabled={busy}>
                <Clock aria-hidden="true" className="size-3.5" />
                {t(($) => $.snooze.action)}
              </Button>
            }
          />
          <DropdownMenuContent align="end" className="w-56">
            {SNOOZE_PRESETS.map((preset) => (
              <DropdownMenuItem
                key={preset}
                onClick={() => void handleSnooze(resolveSnoozePreset(preset, new Date()))}
              >
                {t(($) => $.snooze.preset[preset])}
              </DropdownMenuItem>
            ))}
            <div className="flex flex-col gap-1.5 border-t px-2 py-2">
              <label className="text-caption text-muted-foreground" htmlFor="triage-snooze-custom">
                {t(($) => $.snooze.custom)}
              </label>
              <Input
                id="triage-snooze-custom"
                type="datetime-local"
                value={customSnooze}
                onChange={(e) => setCustomSnooze(e.target.value)}
              />
              <Button
                size="sm"
                variant="outline"
                onClick={() => {
                  const iso = customSnoozeIso(customSnooze, new Date());
                  if (!iso) {
                    toast.error(t(($) => $.snooze.invalid));
                    return;
                  }
                  void handleSnooze(new Date(iso));
                }}
              >
                {t(($) => $.snooze.confirm)}
              </Button>
            </div>
          </DropdownMenuContent>
        </DropdownMenu>

        <Button size="sm" onClick={handleAccept} disabled={busy}>
          {accept.isPending ? (
            <Loader2 aria-hidden="true" className="size-3.5 animate-spin" />
          ) : (
            <Check aria-hidden="true" className="size-3.5" />
          )}
          {accept.isPending ? t(($) => $.detail.accepting) : t(($) => $.detail.accept)}
        </Button>
      </div>

      {mergeOpen ? (
        <IssuePickerModal
          open
          onOpenChange={setMergeOpen}
          title={t(($) => $.merge.title)}
          description={t(($) => $.merge.description)}
          excludeIds={[]}
          onSelect={(issue) => {
            void merge
              .mutateAsync({ itemId: item.id, issueId: issue.id })
              .then(() => {
                toast.success(
                  t(($) => $.merge.toast, { identifier: issue.identifier }),
                );
                onResolved();
              })
              .catch(() => toast.error(t(($) => $.detail.error_toast)));
          }}
        />
      ) : null}
    </div>
  );
}

/**
 * Shared accept-failure mapping: a duplicate the server folded for us and a
 * quota refusal are distinct outcomes the user must be able to tell apart.
 */
export function handleTriageAcceptError(
  err: unknown,
  t: ReturnType<typeof useT<"triage">>["t"],
) {
  if (err instanceof ApiError) {
    const body = (err.body ?? {}) as { code?: string; duplicate_issue_identifier?: string };
    if (err.status === 409 && body.code === "duplicate") {
      toast.info(
        t(($) => $.detail.merged_toast, {
          identifier: body.duplicate_issue_identifier ?? "",
        }),
      );
      return;
    }
    if (err.status === 402) {
      toast.error(t(($) => $.detail.limit_toast));
      return;
    }
  }
  toast.error(t(($) => $.detail.error_toast));
}
