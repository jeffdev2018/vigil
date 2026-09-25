"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { GitBranch, Loader2, Server } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useWorkspacePaths } from "@multica/core/paths";
import { workspaceKeys } from "@multica/core/workspace/queries";
import {
  BRANCH_GC_SETTINGS_KEY,
  BRANCH_GC_TTL_MAX,
  BRANCH_GC_TTL_MIN,
  branchGcPolicy,
  deadBranchesOptions,
  groupDeadBranchesByRuntime,
  useDiscardDeadBranches,
  type BranchGcPolicy,
  type DeadBranchEntry,
} from "@multica/core/runs";
import type { Workspace } from "@multica/core/types";
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
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Switch } from "@multica/ui/components/ui/switch";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../../navigation";
import { useT, useTimeAgo } from "../../i18n";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";

/** Max task IDs the discard endpoint accepts per call (frozen contract). */
const DISCARD_BATCH_MAX = 200;

// Dead run-branch cleanup (JEF-388). Auto-GC persists through the workspace
// settings JSONB (`branch_gc` key) via the plain workspace update — the same
// mechanism triage-auto and the approval gates use; there is no dedicated
// endpoint. The retroactive pass lists dead branches and enqueues discards
// the daemons execute asynchronously, so the plan query is invalidated on
// settled and enqueued rows come back with skip_reason "action_pending".
export function BranchCleanupSetting({
  workspace,
  canEdit,
}: {
  workspace: Workspace;
  canEdit: boolean;
}) {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const policy = branchGcPolicy(workspace);
  const [ttl, setTtl] = useState(String(policy.ttl_days));
  const [saving, setSaving] = useState(false);

  async function persist(next: BranchGcPolicy) {
    if (saving) return;
    setSaving(true);
    try {
      const merged = {
        ...((workspace.settings as Record<string, unknown>) ?? {}),
        [BRANCH_GC_SETTINGS_KEY]: next,
      };
      const updated = await api.updateWorkspace(workspace.id, { settings: merged });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
      toast.success(t(($) => $.auto_save.toast_saved), { id: "settings-auto-save" });
    } catch (e) {
      toast.error(
        e instanceof Error && e.message ? e.message : t(($) => $.workspace.branch_gc_failed),
      );
    } finally {
      setSaving(false);
    }
  }

  const commitTtl = () => {
    const parsed = Math.floor(Number(ttl));
    const clamped = Number.isFinite(parsed)
      ? Math.min(BRANCH_GC_TTL_MAX, Math.max(BRANCH_GC_TTL_MIN, parsed))
      : policy.ttl_days;
    setTtl(String(clamped));
    if (clamped !== policy.ttl_days) void persist({ ...policy, ttl_days: clamped });
  };

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <GitBranch className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.branch_cleanup_section)}
        </span>
      }
      description={t(($) => $.workspace.branch_cleanup_section_description)}
    >
      <SettingsCard>
        <SettingsRow
          label={t(($) => $.workspace.branch_gc_enabled_label)}
          description={t(($) => $.workspace.branch_gc_enabled_description)}
        >
          <Switch
            aria-label={t(($) => $.workspace.branch_gc_enabled_label)}
            checked={policy.enabled}
            disabled={!canEdit || saving}
            onCheckedChange={(checked: boolean) =>
              void persist({ ...policy, enabled: checked === true })
            }
          />
        </SettingsRow>
        <SettingsRow
          label={t(($) => $.workspace.branch_gc_ttl_label)}
          description={t(($) => $.workspace.branch_gc_ttl_description)}
        >
          <Input
            type="number"
            min={BRANCH_GC_TTL_MIN}
            max={BRANCH_GC_TTL_MAX}
            aria-label={t(($) => $.workspace.branch_gc_ttl_label)}
            className="w-24"
            value={ttl}
            disabled={!canEdit || saving}
            onChange={(e) => setTtl(e.target.value)}
            onBlur={commitTtl}
          />
        </SettingsRow>
      </SettingsCard>
      <DeadBranchPlan workspace={workspace} canEdit={canEdit} />
    </SettingsSection>
  );
}

function DeadBranchPlan({ workspace, canEdit }: { workspace: Workspace; canEdit: boolean }) {
  const { t } = useT("settings");
  const timeAgo = useTimeAgo();
  const paths = useWorkspacePaths();
  const { data, isPending, isError } = useQuery(deadBranchesOptions(workspace.id));
  const discard = useDiscardDeadBranches(workspace.id);
  const [confirm, setConfirm] = useState<
    { kind: "all" } | { kind: "one"; entry: DeadBranchEntry } | null
  >(null);

  const entries = data?.entries ?? [];
  const groups = groupDeadBranchesByRuntime(entries);
  const actionableIds = entries.filter((e) => e.actionable).map((e) => e.task_id);

  const reportOutcome = (outcome: { enqueued: number; skipped: unknown[] }) => {
    if (outcome.enqueued > 0) {
      toast.success(
        t(($) => $.workspace.branch_cleanup_enqueued_toast, { count: outcome.enqueued }),
      );
    }
    if (outcome.skipped.length > 0) {
      toast.info(
        t(($) => $.workspace.branch_cleanup_skipped_toast, { count: outcome.skipped.length }),
      );
    }
  };

  const runConfirmed = () => {
    if (!confirm) return;
    const taskIds =
      confirm.kind === "all"
        ? actionableIds.slice(0, DISCARD_BATCH_MAX)
        : [confirm.entry.task_id];
    discard.mutate(
      { taskIds },
      {
        onSuccess: reportOutcome,
        onError: (e) =>
          toast.error(
            e instanceof Error && e.message
              ? e.message
              : t(($) => $.workspace.branch_cleanup_failed),
          ),
        onSettled: () => setConfirm(null),
      },
    );
  };

  return (
    <>
      <SettingsSection
        title={t(($) => $.workspace.branch_cleanup_retro_section)}
        description={t(($) => $.workspace.branch_cleanup_retro_description)}
        action={
          actionableIds.length > 0 ? (
            <Button
              type="button"
              size="sm"
              variant="destructive"
              disabled={!canEdit || discard.isPending}
              onClick={() => setConfirm({ kind: "all" })}
            >
              {discard.isPending && <Loader2 className="animate-spin" />}
              {t(($) => $.workspace.branch_cleanup_discard_all, { count: actionableIds.length })}
            </Button>
          ) : null
        }
      >
        <SettingsCard>
          {isPending ? (
            <div className="px-4 py-6 text-caption text-muted-foreground">
              {t(($) => $.workspace.branch_cleanup_loading)}
            </div>
          ) : isError ? (
            <div className="px-4 py-6 text-caption text-muted-foreground">
              {t(($) => $.workspace.branch_cleanup_failed)}
            </div>
          ) : entries.length === 0 ? (
            <div
              data-testid="dead-branches-empty"
              className="px-4 py-6 text-caption text-muted-foreground"
            >
              {t(($) => $.workspace.branch_cleanup_empty)}
            </div>
          ) : (
            groups.map((group) => (
              <div key={group.runtimeName}>
                <div className="flex items-center gap-1.5 bg-muted/40 px-4 py-2 text-caption font-medium text-muted-foreground">
                  <Server className="h-3 w-3 shrink-0" aria-hidden="true" />
                  {group.runtimeName}
                </div>
                {group.entries.map((entry) => (
                  <DeadBranchRow
                    key={entry.task_id}
                    entry={entry}
                    issueHref={entry.issue_id ? paths.issueDetail(entry.issue_id) : null}
                    finishedAge={entry.finished_at ? timeAgo(entry.finished_at) : null}
                    canEdit={canEdit}
                    busy={discard.isPending}
                    onDiscard={() => setConfirm({ kind: "one", entry })}
                  />
                ))}
              </div>
            ))
          )}
        </SettingsCard>
      </SettingsSection>

      <AlertDialog
        open={confirm !== null}
        onOpenChange={(open) => {
          if (!open) setConfirm(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.workspace.branch_cleanup_discard_dialog_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {confirm?.kind === "one"
                ? t(($) => $.workspace.branch_cleanup_discard_dialog_body, {
                    branch: confirm.entry.branch_name,
                  })
                : t(($) => $.workspace.branch_cleanup_discard_all_dialog_body, {
                    count: actionableIds.length,
                  })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>
              {t(($) => $.workspace.branch_cleanup_dialog_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={runConfirmed}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {t(($) => $.workspace.branch_cleanup_dialog_confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function DeadBranchRow({
  entry,
  issueHref,
  finishedAge,
  canEdit,
  busy,
  onDiscard,
}: {
  entry: DeadBranchEntry;
  issueHref: string | null;
  finishedAge: string | null;
  canEdit: boolean;
  busy: boolean;
  onDiscard: () => void;
}) {
  const { t } = useT("settings");
  const reason =
    entry.skip_reason === "runtime_offline"
      ? t(($) => $.workspace.branch_cleanup_reason_offline)
      : entry.skip_reason === "capability_missing"
        ? t(($) => $.workspace.branch_cleanup_reason_capability)
        : entry.skip_reason === "action_pending"
          ? t(($) => $.workspace.branch_cleanup_reason_pending)
          : null;

  return (
    <div
      data-testid="dead-branch-row"
      className={cn(
        "flex items-center gap-3 px-4 py-3",
        !entry.actionable && "opacity-60",
      )}
    >
      <div className="min-w-0 flex-1 space-y-0.5">
        <div
          className="truncate font-mono text-caption"
          title={entry.branch_name}
        >
          {entry.branch_name}
        </div>
        <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-caption text-muted-foreground">
          {issueHref && entry.issue_identifier ? (
            <AppLink
              href={issueHref}
              className="underline-offset-4 hover:text-foreground hover:underline"
            >
              {entry.issue_identifier}
              {entry.issue_title ? ` — ${entry.issue_title}` : ""}
            </AppLink>
          ) : entry.issue_identifier ? (
            <span>
              {entry.issue_identifier}
              {entry.issue_title ? ` — ${entry.issue_title}` : ""}
            </span>
          ) : null}
          {finishedAge ? <span>{finishedAge}</span> : null}
          {reason ? <span>{reason}</span> : null}
        </div>
      </div>
      {entry.actionable ? (
        <Button
          type="button"
          size="sm"
          variant="ghost"
          className="shrink-0 text-destructive hover:bg-destructive/10 hover:text-destructive"
          disabled={!canEdit || busy}
          onClick={onDiscard}
        >
          {t(($) => $.workspace.branch_cleanup_discard_branch)}
        </Button>
      ) : null}
    </div>
  );
}
