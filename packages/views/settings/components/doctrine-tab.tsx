"use client";

import { useState } from "react";
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, ScrollText } from "lucide-react";
import { toast } from "sonner";
import { ApiError, api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import {
  agentListOptions,
  memberListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import {
  DOCTRINE_REPORT_FILTERS,
  EMPTY_DOCTRINE,
  canPublishDoctrine,
  doctrineByteLength,
  doctrineDiffOptions,
  doctrineKeys,
  doctrineOptions,
  doctrineReportsOptions,
  doctrineVersionsInfiniteOptions,
  useAcknowledgeDoctrineReport,
  useApproveDoctrineVersion,
  useDismissDoctrineReport,
  usePublishDoctrine,
  useRejectDoctrineVersion,
  useRestoreDoctrineVersion,
  type Doctrine,
  type DoctrineReport,
  type DoctrineReportFilter,
  type DoctrineVersion,
} from "@multica/core/doctrine";
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../../navigation";
import { useT, useTimeAgo } from "../../i18n";
import {
  SettingsCard,
  SettingsRow,
  SettingsSaveState,
  SettingsSection,
  SettingsTab,
  type SettingsSaveStatus,
} from "./settings-layout";

/**
 * Workspace doctrine (OS plan, chantier 22).
 *
 * The old Settings → General "Context" textarea was a governing document with
 * no ceremony: auto-saved on blur, last writer wins, no trace of who changed
 * what. This tab gives it the shape a governing document needs — an explicit
 * Publish, an optional second reviewer, a revision ledger with a line diff and
 * restore, and the reports agents file when a task collides with a rule.
 *
 * Nothing here is optimistic: publish, approve, reject and restore all move
 * the document every agent reads, so each awaits the server (CLAUDE.md state
 * rules).
 */

const DOCTRINE_SETTING_KEY = "doctrine";

const VERSION_STATUS_TONE: Record<string, string> = {
  active: "bg-success/10 text-success",
  pending: "bg-warning/10 text-warning",
  rejected: "bg-destructive/10 text-destructive",
  superseded: "bg-muted text-muted-foreground",
};

const REPORT_KIND_TONE: Record<string, string> = {
  conflict: "bg-destructive/10 text-destructive",
  refusal: "bg-warning/10 text-warning",
  ambiguity: "bg-muted text-muted-foreground",
};

const DIFF_LINE_TONE: Record<string, string> = {
  add: "bg-success/10 text-success",
  del: "bg-destructive/10 text-destructive",
  same: "text-muted-foreground",
};

const SELECT_CLASS =
  "rounded-md border border-input bg-transparent px-2 py-1 text-caption";

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

/** A publication the server refused because the document moved under us. */
function isStaleDoctrine(error: unknown): boolean {
  return error instanceof ApiError && error.status === 409;
}

function shortId(id: string): string {
  return id ? id.slice(0, 8) : "";
}

function Pill({ tone, children }: { tone: string; children: React.ReactNode }) {
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center rounded-full px-2 py-0.5 text-caption font-medium",
        tone,
      )}
    >
      {children}
    </span>
  );
}

export function DoctrineTab() {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const timeAgo = useTimeAgo();
  const paths = useWorkspacePaths();
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";
  const userId = useAuthStore((s) => s.user)?.id ?? "";

  const doctrineQuery = useQuery(doctrineOptions(wsId));
  const doctrine: Doctrine = doctrineQuery.data ?? EMPTY_DOCTRINE;
  const versionsQuery = useInfiniteQuery(doctrineVersionsInfiniteOptions(wsId));
  const versions = versionsQuery.data?.pages.flatMap((page) => page.versions) ?? [];

  const publish = usePublishDoctrine(wsId);
  const approve = useApproveDoctrineVersion(wsId);
  const reject = useRejectDoctrineVersion(wsId);
  const restore = useRestoreDoctrineVersion(wsId);

  // The draft stays null until the manager types, so a refetch (a websocket
  // `doctrine:changed`, a window refocus) never overwrites what they wrote.
  const [draft, setDraft] = useState<string | null>(null);
  const [note, setNote] = useState("");
  const [stale, setStale] = useState(false);
  const [policySaving, setPolicySaving] = useState<SettingsSaveStatus>("idle");
  const [diffTarget, setDiffTarget] = useState<DoctrineVersion | null>(null);
  const [restoreTarget, setRestoreTarget] = useState<DoctrineVersion | null>(null);

  const content = draft ?? doctrine.content;
  const used = doctrineByteLength(content);
  const overLimit = used > doctrine.byte_limit;
  const canEdit = doctrine.can_publish === true;
  const publishable = canPublishDoctrine(doctrine, content) && !stale;

  const names = useDoctrineNames(wsId);

  const reload = () => {
    setDraft(null);
    setStale(false);
    void qc.invalidateQueries({ queryKey: doctrineKeys.all(wsId) });
  };

  const handlePublish = () => {
    publish.mutate(
      { content, expected_revision: doctrine.revision, note: note.trim() },
      {
        onSuccess: (result) => {
          setDraft(null);
          setNote("");
          toast.success(
            result.doctrine.pending
              ? t(($) => $.doctrine.proposed_toast)
              : t(($) => $.doctrine.published_toast),
          );
        },
        onError: (error) => {
          if (isStaleDoctrine(error)) {
            setStale(true);
            return;
          }
          toast.error(errorMessage(error, t(($) => $.doctrine.publish_failed)));
        },
      },
    );
  };

  const savePolicy = async (requireReview: boolean) => {
    if (!workspace) return;
    setPolicySaving("saving");
    try {
      const merged = {
        ...((workspace.settings as Record<string, unknown> | null) ?? {}),
        [DOCTRINE_SETTING_KEY]: { require_review: requireReview },
      };
      const updated = await api.updateWorkspace(workspace.id, { settings: merged });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
      await qc.invalidateQueries({ queryKey: doctrineKeys.all(wsId) });
      setPolicySaving("saved");
    } catch (error) {
      setPolicySaving("error");
      toast.error(errorMessage(error, t(($) => $.doctrine.policy_failed)));
    }
  };

  const pending = doctrine.pending;
  const pendingIsMine = !!pending && !!pending.author_id && pending.author_id === userId;

  return (
    <SettingsTab
      title={t(($) => $.doctrine.title)}
      description={t(($) => $.doctrine.description)}
    >
      <SettingsCard>
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3.5">
          <span className="inline-flex items-center gap-2 text-body font-medium">
            <ScrollText aria-hidden="true" className="size-4 text-muted-foreground" />
            {t(($) => $.doctrine.revision, { revision: doctrine.revision })}
          </span>
          <span className="text-caption text-muted-foreground" data-testid="doctrine-updated">
            {doctrine.updated_at
              ? doctrine.updated_by
                ? t(($) => $.doctrine.updated_at_by, {
                    when: timeAgo(doctrine.updated_at),
                    name: names(doctrine.updated_by),
                  })
                : t(($) => $.doctrine.updated_at, { when: timeAgo(doctrine.updated_at) })
              : t(($) => $.doctrine.updated_never)}
          </span>
          <span
            className={cn(
              "text-caption tabular-nums",
              overLimit ? "text-destructive" : "text-muted-foreground",
            )}
            data-testid="doctrine-bytes"
          >
            {t(($) => $.doctrine.bytes, { used, limit: doctrine.byte_limit })}
          </span>
          {doctrine.open_reports > 0 ? (
            <Pill tone="bg-warning/10 text-warning">
              {t(($) => $.doctrine.open_reports, { count: doctrine.open_reports })}
            </Pill>
          ) : null}
        </div>
      </SettingsCard>

      {pending ? (
        <PendingProposalCard
          pending={pending}
          isAuthor={pendingIsMine}
          canReview={canEdit && !pendingIsMine}
          authorName={names(pending.author_id)}
          reviewing={approve.isPending || reject.isPending}
          onViewChanges={() => setDiffTarget(pending)}
          onApprove={(reviewNote) =>
            approve.mutate(
              { id: pending.id, note: reviewNote },
              {
                onSuccess: () => toast.success(t(($) => $.doctrine.approved_toast)),
                onError: (error) =>
                  toast.error(errorMessage(error, t(($) => $.doctrine.review_failed))),
              },
            )
          }
          onReject={(reviewNote) =>
            reject.mutate(
              { id: pending.id, note: reviewNote },
              {
                onSuccess: () => toast.success(t(($) => $.doctrine.rejected_toast)),
                onError: (error) =>
                  toast.error(errorMessage(error, t(($) => $.doctrine.review_failed))),
              },
            )
          }
        />
      ) : null}

      <SettingsSection
        title={t(($) => $.doctrine.editor_title)}
        description={
          canEdit
            ? t(($) => $.doctrine.editor_description)
            : t(($) => $.doctrine.read_only_notice)
        }
      >
        <SettingsCard>
          <div className="space-y-3 px-4 py-4">
            {canEdit ? (
              <>
                <Label htmlFor="doctrine-content">{t(($) => $.doctrine.content_label)}</Label>
                <Textarea
                  id="doctrine-content"
                  name="workspace-doctrine"
                  autoComplete="off"
                  spellCheck={false}
                  rows={12}
                  className="min-h-64 font-mono text-caption leading-6"
                  placeholder={t(($) => $.doctrine.content_placeholder)}
                  value={content}
                  disabled={!!pending || publish.isPending}
                  onChange={(event) => setDraft(event.target.value)}
                />
              </>
            ) : (
              <pre
                className="max-h-[32rem] overflow-auto whitespace-pre-wrap break-words font-mono text-caption leading-6 text-foreground"
                data-testid="doctrine-readonly"
              >
                {doctrine.content}
              </pre>
            )}

            {overLimit ? (
              <p className="text-caption text-destructive" data-testid="doctrine-over-limit">
                {t(($) => $.doctrine.over_limit, { limit: doctrine.byte_limit })}
              </p>
            ) : null}

            {stale ? (
              <div
                className="flex flex-wrap items-center gap-3 rounded-md bg-destructive/10 px-3 py-2"
                data-testid="doctrine-stale"
              >
                <span className="inline-flex items-center gap-2 text-caption text-destructive">
                  <AlertTriangle aria-hidden="true" className="size-3.5" />
                  {t(($) => $.doctrine.stale_notice)}
                </span>
                <Button size="sm" variant="outline" onClick={reload}>
                  {t(($) => $.doctrine.reload)}
                </Button>
              </div>
            ) : null}

            {canEdit ? (
              <div className="space-y-2">
                <Label htmlFor="doctrine-note">{t(($) => $.doctrine.note_label)}</Label>
                <Input
                  id="doctrine-note"
                  value={note}
                  disabled={!!pending || publish.isPending}
                  placeholder={t(($) => $.doctrine.note_placeholder)}
                  onChange={(event) => setNote(event.target.value)}
                />
                <div className="flex items-center gap-3 pt-1">
                  <Button
                    size="sm"
                    data-testid="doctrine-publish"
                    disabled={!publishable || publish.isPending}
                    onClick={handlePublish}
                  >
                    {t(($) => $.doctrine.publish)}
                  </Button>
                  <SettingsSaveState
                    status={publish.isPending ? "saving" : "idle"}
                    savingLabel={t(($) => $.auto_save.saving)}
                    savedLabel={t(($) => $.auto_save.saved)}
                    errorLabel={t(($) => $.auto_save.failed)}
                  />
                </div>
              </div>
            ) : null}
          </div>
        </SettingsCard>
      </SettingsSection>

      <SettingsSection
        title={t(($) => $.doctrine.policy_title)}
        description={t(($) => $.doctrine.policy_description)}
        action={
          <SettingsSaveState
            status={policySaving}
            savingLabel={t(($) => $.auto_save.saving)}
            savedLabel={t(($) => $.auto_save.saved)}
            errorLabel={t(($) => $.auto_save.failed)}
          />
        }
      >
        <SettingsCard>
          <SettingsRow
            label={t(($) => $.doctrine.require_review_label)}
            description={t(($) => $.doctrine.require_review_description)}
          >
            <Switch
              aria-label={t(($) => $.doctrine.require_review_label)}
              data-testid="doctrine-require-review"
              checked={doctrine.require_review === true}
              disabled={!canEdit || policySaving === "saving"}
              onCheckedChange={(checked: boolean) => void savePolicy(checked)}
            />
          </SettingsRow>
        </SettingsCard>
      </SettingsSection>

      <SettingsSection
        title={t(($) => $.doctrine.history_title)}
        description={t(($) => $.doctrine.history_description)}
      >
        <SettingsCard>
          {versions.length === 0 ? (
            <p
              className="px-4 py-8 text-center text-caption text-muted-foreground"
              data-testid="doctrine-history-empty"
            >
              {t(($) => $.doctrine.history_empty)}
            </p>
          ) : (
            <ul className="divide-y divide-surface-border">
              {versions.map((version) => (
                <li
                  key={version.id}
                  className="flex flex-wrap items-start justify-between gap-3 px-4 py-3"
                  data-testid="doctrine-version-row"
                >
                  <div className="min-w-0 space-y-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <Pill
                        tone={
                          VERSION_STATUS_TONE[version.status] ??
                          "bg-muted text-muted-foreground"
                        }
                      >
                        {versionStatusLabel(version.status, t)}
                      </Pill>
                      {version.revision !== null ? (
                        <span className="text-body font-medium tabular-nums">
                          {t(($) => $.doctrine.revision, { revision: version.revision })}
                        </span>
                      ) : null}
                      <span className="text-caption text-muted-foreground">
                        {names(version.author_id)}
                      </span>
                      <span className="text-caption text-muted-foreground">
                        {timeAgo(version.created_at)}
                      </span>
                      <span className="text-caption text-muted-foreground tabular-nums">
                        {t(($) => $.doctrine.bytes_short, { bytes: version.bytes })}
                      </span>
                    </div>
                    {version.note ? (
                      <p className="text-caption text-muted-foreground">{version.note}</p>
                    ) : null}
                    {version.restored_from_revision !== null ? (
                      <p className="text-caption text-muted-foreground">
                        {t(($) => $.doctrine.restored_from, {
                          revision: version.restored_from_revision,
                        })}
                      </p>
                    ) : null}
                  </div>
                  <div className="flex shrink-0 gap-2">
                    <Button
                      size="sm"
                      variant="ghost"
                      data-testid="doctrine-compare"
                      onClick={() => setDiffTarget(version)}
                    >
                      {t(($) => $.doctrine.compare)}
                    </Button>
                    {canEdit && version.revision !== null && version.status !== "active" ? (
                      <Button
                        size="sm"
                        variant="outline"
                        data-testid="doctrine-restore"
                        disabled={restore.isPending}
                        onClick={() => setRestoreTarget(version)}
                      >
                        {t(($) => $.doctrine.restore)}
                      </Button>
                    ) : null}
                  </div>
                </li>
              ))}
            </ul>
          )}
          {versionsQuery.hasNextPage ? (
            <div className="px-4 py-3">
              <Button
                size="sm"
                variant="ghost"
                disabled={versionsQuery.isFetchingNextPage}
                onClick={() => void versionsQuery.fetchNextPage()}
              >
                {t(($) => $.doctrine.load_more)}
              </Button>
            </div>
          ) : null}
        </SettingsCard>
      </SettingsSection>

      <ReportsSection wsId={wsId} canResolve={canEdit} names={names} issueHref={paths.issueDetail} />

      <Dialog
        open={!!diffTarget}
        onOpenChange={(open: boolean) => {
          if (!open) setDiffTarget(null);
        }}
      >
        <DialogContent className="max-w-3xl">
          {diffTarget ? <DiffBody wsId={wsId} version={diffTarget} /> : null}
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={!!restoreTarget}
        onOpenChange={(open: boolean) => {
          if (!open) setRestoreTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.doctrine.restore_title, { revision: restoreTarget?.revision ?? 0 })}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.doctrine.restore_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.doctrine.cancel)}</AlertDialogCancel>
            <AlertDialogAction
              data-testid="doctrine-restore-confirm"
              onClick={() => {
                const target = restoreTarget;
                setRestoreTarget(null);
                if (!target) return;
                restore.mutate(
                  { id: target.id, expected_revision: doctrine.revision },
                  {
                    onSuccess: () => {
                      setDraft(null);
                      toast.success(t(($) => $.doctrine.restored_toast));
                    },
                    onError: (error) => {
                      if (isStaleDoctrine(error)) {
                        setStale(true);
                        return;
                      }
                      toast.error(
                        errorMessage(error, t(($) => $.doctrine.restore_failed)),
                      );
                    },
                  },
                );
              }}
            >
              {t(($) => $.doctrine.restore_confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsTab>
  );
}

/**
 * Resolves a member or agent id to a name from the lists the workspace
 * already has cached; falls back to a short id so a row is never blank.
 */
function useDoctrineNames(wsId: string): (id: string | null | undefined) => string {
  const { t } = useT("settings");
  const { data: members = [] } = useQuery({ ...memberListOptions(wsId), enabled: !!wsId });
  const { data: agents = [] } = useQuery({ ...agentListOptions(wsId), enabled: !!wsId });
  return (id) => {
    if (!id) return t(($) => $.doctrine.unknown_member);
    const member = members.find((m) => m.user_id === id);
    if (member?.name) return member.name;
    const agent = agents.find((a) => a.id === id);
    if (agent?.name) return agent.name;
    return shortId(id);
  };
}

type Translate = ReturnType<typeof useT<"settings">>["t"];

function versionStatusLabel(status: string, t: Translate): string {
  switch (status) {
    case "active":
      return t(($) => $.doctrine.status_active);
    case "pending":
      return t(($) => $.doctrine.status_pending);
    case "rejected":
      return t(($) => $.doctrine.status_rejected);
    case "superseded":
      return t(($) => $.doctrine.status_superseded);
    default:
      return t(($) => $.doctrine.status_unknown);
  }
}

function reportKindLabel(kind: string, t: Translate): string {
  switch (kind) {
    case "conflict":
      return t(($) => $.doctrine.kind_conflict);
    case "refusal":
      return t(($) => $.doctrine.kind_refusal);
    case "ambiguity":
      return t(($) => $.doctrine.kind_ambiguity);
    default:
      return t(($) => $.doctrine.kind_unknown);
  }
}

function reportStatusLabel(status: string, t: Translate): string {
  switch (status) {
    case "acknowledged":
      return t(($) => $.doctrine.report_status_acknowledged);
    case "dismissed":
      return t(($) => $.doctrine.report_status_dismissed);
    default:
      return t(($) => $.doctrine.report_status_open);
  }
}

function reportFilterLabel(filter: DoctrineReportFilter, t: Translate): string {
  switch (filter) {
    case "acknowledged":
      return t(($) => $.doctrine.filter_acknowledged);
    case "dismissed":
      return t(($) => $.doctrine.filter_dismissed);
    case "all":
      return t(($) => $.doctrine.filter_all);
    default:
      return t(($) => $.doctrine.filter_open);
  }
}

function PendingProposalCard({
  pending,
  isAuthor,
  canReview,
  authorName,
  reviewing,
  onViewChanges,
  onApprove,
  onReject,
}: {
  pending: DoctrineVersion;
  isAuthor: boolean;
  canReview: boolean;
  authorName: string;
  reviewing: boolean;
  onViewChanges: () => void;
  onApprove: (note: string) => void;
  onReject: (note: string) => void;
}) {
  const { t } = useT("settings");
  const [reviewNote, setReviewNote] = useState("");

  return (
    <SettingsCard className="border-warning/40">
      <div className="space-y-3 px-4 py-4" data-testid="doctrine-pending">
        <div className="flex flex-wrap items-center gap-2">
          <Pill tone="bg-warning/10 text-warning">{t(($) => $.doctrine.pending_title)}</Pill>
          <span className="text-body font-medium">
            {t(($) => $.doctrine.pending_by, { name: authorName })}
          </span>
        </div>
        {pending.note ? (
          <p className="text-caption text-muted-foreground">{pending.note}</p>
        ) : null}
        {isAuthor ? (
          <p className="text-caption text-muted-foreground" data-testid="doctrine-pending-awaiting">
            {t(($) => $.doctrine.pending_awaiting)}
          </p>
        ) : null}
        {canReview ? (
          <div className="space-y-2">
            <Label htmlFor="doctrine-review-note">
              {t(($) => $.doctrine.review_note_label)}
            </Label>
            <Input
              id="doctrine-review-note"
              value={reviewNote}
              placeholder={t(($) => $.doctrine.review_note_placeholder)}
              onChange={(event) => setReviewNote(event.target.value)}
            />
          </div>
        ) : null}
        <div className="flex flex-wrap gap-2">
          <Button
            size="sm"
            variant="outline"
            data-testid="doctrine-pending-diff"
            onClick={onViewChanges}
          >
            {t(($) => $.doctrine.view_changes)}
          </Button>
          {canReview ? (
            <>
              <Button
                size="sm"
                data-testid="doctrine-approve"
                disabled={reviewing}
                onClick={() => onApprove(reviewNote.trim())}
              >
                {t(($) => $.doctrine.approve)}
              </Button>
              <Button
                size="sm"
                variant="outline"
                data-testid="doctrine-reject"
                disabled={reviewing}
                onClick={() => onReject(reviewNote.trim())}
              >
                {t(($) => $.doctrine.reject)}
              </Button>
            </>
          ) : null}
        </div>
      </div>
    </SettingsCard>
  );
}

function DiffBody({ wsId, version }: { wsId: string; version: DoctrineVersion }) {
  const { t } = useT("settings");
  const diffQuery = useQuery(doctrineDiffOptions(wsId, version.id));
  const diff = diffQuery.data;
  const lines = diff?.lines ?? [];

  return (
    <>
      <DialogHeader>
        <DialogTitle>{t(($) => $.doctrine.diff_title)}</DialogTitle>
        <DialogDescription>
          {diff
            ? `${
                diff.from?.revision != null
                  ? t(($) => $.doctrine.diff_against_revision, {
                      revision: diff.from?.revision ?? 0,
                    })
                  : t(($) => $.doctrine.diff_against_live)
              } · ${t(($) => $.doctrine.diff_summary, {
                added: diff.added,
                removed: diff.removed,
              })}`
            : t(($) => $.doctrine.diff_title)}
        </DialogDescription>
      </DialogHeader>
      {lines.length === 0 ? (
        <p className="text-caption text-muted-foreground" data-testid="doctrine-diff-identical">
          {t(($) => $.doctrine.diff_identical)}
        </p>
      ) : (
        <div className="max-h-[26rem] overflow-auto rounded-md border border-surface-border">
          <ul className="divide-y divide-surface-border/60">
            {lines.map((line, index) => (
              <li
                key={`${index}-${line.kind}`}
                data-kind={line.kind}
                data-testid="doctrine-diff-line"
                className={cn(
                  "flex gap-2 px-3 py-1 font-mono text-caption leading-5",
                  DIFF_LINE_TONE[line.kind] ?? "text-muted-foreground",
                )}
              >
                <span aria-hidden="true" className="w-3 shrink-0 select-none">
                  {line.kind === "add" ? "+" : line.kind === "del" ? "−" : " "}
                </span>
                <span className="min-w-0 whitespace-pre-wrap break-words">{line.text}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </>
  );
}

function ReportsSection({
  wsId,
  canResolve,
  names,
  issueHref,
}: {
  wsId: string;
  canResolve: boolean;
  names: (id: string | null | undefined) => string;
  issueHref: (id: string) => string;
}) {
  const { t } = useT("settings");
  const [filter, setFilter] = useState<DoctrineReportFilter>("open");
  const reportsQuery = useQuery(doctrineReportsOptions(wsId, filter));
  const reports = reportsQuery.data ?? [];

  return (
    <SettingsSection
      title={t(($) => $.doctrine.reports_title)}
      description={t(($) => $.doctrine.reports_description)}
      action={
        <select
          aria-label={t(($) => $.doctrine.filter_label)}
          className={SELECT_CLASS}
          data-testid="doctrine-reports-filter"
          value={filter}
          onChange={(event) => setFilter(event.target.value as DoctrineReportFilter)}
        >
          {DOCTRINE_REPORT_FILTERS.map((value) => (
            <option key={value} value={value}>
              {reportFilterLabel(value, t)}
            </option>
          ))}
        </select>
      }
    >
      <SettingsCard>
        {reports.length === 0 ? (
          <p
            className="px-4 py-8 text-center text-caption text-muted-foreground"
            data-testid="doctrine-reports-empty"
          >
            {t(($) => $.doctrine.reports_empty)}
          </p>
        ) : (
          <ul className="divide-y divide-surface-border">
            {reports.map((report) => (
              <ReportRow
                key={report.id}
                wsId={wsId}
                report={report}
                canResolve={canResolve}
                reporterName={names(report.reporter_id)}
                issueHref={issueHref}
              />
            ))}
          </ul>
        )}
      </SettingsCard>
    </SettingsSection>
  );
}

function ReportRow({
  wsId,
  report,
  canResolve,
  reporterName,
  issueHref,
}: {
  wsId: string;
  report: DoctrineReport;
  canResolve: boolean;
  reporterName: string;
  issueHref: (id: string) => string;
}) {
  const { t } = useT("settings");
  const timeAgo = useTimeAgo();
  const acknowledge = useAcknowledgeDoctrineReport(wsId);
  const dismiss = useDismissDoctrineReport(wsId);
  const [note, setNote] = useState("");
  const open = report.status === "open";
  const busy = acknowledge.isPending || dismiss.isPending;

  const handlers = {
    onSuccess: () => toast.success(t(($) => $.doctrine.report_resolved_toast)),
    onError: (error: unknown) =>
      toast.error(errorMessage(error, t(($) => $.doctrine.report_failed))),
  };

  return (
    <li className="space-y-2 px-4 py-3" data-testid="doctrine-report-row">
      <div className="flex flex-wrap items-center gap-2">
        <Pill tone={REPORT_KIND_TONE[report.kind] ?? "bg-muted text-muted-foreground"}>
          {reportKindLabel(report.kind, t)}
        </Pill>
        {!open ? (
          <span className="text-caption text-muted-foreground" data-testid="doctrine-report-status">
            {reportStatusLabel(report.status, t)}
          </span>
        ) : null}
        <span className="text-caption text-muted-foreground">
          {t(($) => $.doctrine.report_revision, { revision: report.doctrine_revision })}
        </span>
        <span className="text-caption text-muted-foreground">
          {t(($) => $.doctrine.report_reporter, { name: reporterName })}
        </span>
        <span className="text-caption text-muted-foreground">{timeAgo(report.created_at)}</span>
      </div>
      <p className="text-body">{report.summary}</p>
      {report.passage ? (
        <blockquote className="border-l-2 border-surface-border pl-3 text-caption whitespace-pre-wrap text-muted-foreground">
          {report.passage}
        </blockquote>
      ) : null}
      {report.resolution_note ? (
        <p className="text-caption text-muted-foreground">{report.resolution_note}</p>
      ) : null}
      <div className="flex flex-wrap items-center gap-2">
        {report.issue_id ? (
          <AppLink
            href={issueHref(report.issue_id)}
            className="text-caption text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
            data-testid="doctrine-report-issue"
          >
            {t(($) => $.doctrine.report_open_issue)}
          </AppLink>
        ) : null}
        {canResolve && open ? (
          <>
            <Input
              aria-label={t(($) => $.doctrine.report_note_label)}
              data-testid="doctrine-report-note"
              className="h-8 w-56"
              value={note}
              placeholder={t(($) => $.doctrine.report_note_placeholder)}
              onChange={(event) => setNote(event.target.value)}
            />
            <Button
              size="sm"
              data-testid="doctrine-report-acknowledge"
              disabled={busy}
              onClick={() => acknowledge.mutate({ id: report.id, note: note.trim() }, handlers)}
            >
              {t(($) => $.doctrine.acknowledge)}
            </Button>
            <Button
              size="sm"
              variant="outline"
              data-testid="doctrine-report-dismiss"
              disabled={busy}
              onClick={() => dismiss.mutate({ id: report.id, note: note.trim() }, handlers)}
            >
              {t(($) => $.doctrine.dismiss)}
            </Button>
          </>
        ) : null}
      </div>
    </li>
  );
}
