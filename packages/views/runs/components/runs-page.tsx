"use client";

import { useCallback, useMemo, useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Server, ShieldOff, Loader2, Ban } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useAuthStore } from "@multica/core/auth";
import { api } from "@multica/core/api";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import { runtimeListOptions, runtimeDisplayLabel } from "@multica/core/runtimes";
import { runHaltOptions, useSetRunHalt, EMPTY_RUN_HALT } from "@multica/core/run-halt";
import {
  workspaceRunsInfiniteOptions,
  useCancelRuns,
  useKillSwitch,
  isRunSilent,
  runCostUsd,
  runCostKnown,
  blockerHash,
  EMPTY_RUNS_SUMMARY,
  type Run,
  type RunsSummary,
  type RunCancelOutcome,
} from "@multica/core/runs";
import type { AgentTask, TaskStatus } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { useT, useTimeAgo } from "../../i18n";
import { CollectionPageHeader, CollectionPageState } from "../../layout/collection-page";
import { AppLink } from "../../navigation";
import { SegmentedToggle } from "../../common/segmented-toggle";
import { ActorAvatar } from "../../common/actor-avatar";
import { ReplayButton, TranscriptButton } from "../../common/task-transcript";
import { TaskStatusIcon } from "../../issues/components/task-status-icon";
import { useStatusLabel } from "../../issues/components/task-run-labels";
import { formatDurationMs } from "../../agents/components/tabs/activity-tab";
import { formatUsd } from "../../runtimes/utils";

// Same tone map as ExecutionLogSection's STATUS_TONE — kept local rather than
// exported cross-domain for one shared const; both must agree on the color a
// status reads as, wherever a run row appears.
const STATUS_TONE: Record<TaskStatus, string> = {
  queued: "text-warning",
  deferred: "text-warning",
  dispatched: "text-warning",
  waiting_local_directory: "text-warning",
  running: "text-info",
  completed: "text-success",
  failed: "text-destructive",
  cancelled: "text-muted-foreground",
  paused: "text-warning",
};

// A settled run has nothing left to cancel; the row keeps transcript and replay only.
function isSettledRunStatus(status: string): boolean {
  return status === "completed" || status === "failed" || status === "cancelled";
}

type StateFilter = "in_flight" | "finished" | "all";
const STATE_TO_API: Record<StateFilter, "active" | "terminal" | "all"> = {
  in_flight: "active",
  finished: "terminal",
  all: "all",
};

const ISSUE_UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function RunsPage() {
  const wsId = useWorkspaceId();
  const { t } = useT("runs");
  const wsPaths = useWorkspacePaths();

  const [stateFilter, setStateFilter] = useState<StateFilter>("in_flight");
  const [agentId, setAgentId] = useState<string | undefined>(undefined);
  const [runtimeId, setRuntimeId] = useState<string | undefined>(undefined);
  const [issueId, setIssueId] = useState<string | undefined>(undefined);
  const [checkedIds, setCheckedIds] = useState<Set<string>>(() => new Set());

  const filter = useMemo(
    () => ({ state: STATE_TO_API[stateFilter], agentId, runtimeId, issueId }),
    [stateFilter, agentId, runtimeId, issueId],
  );

  const runsQuery = useInfiniteQuery(workspaceRunsInfiniteOptions(wsId, filter));
  const runs = useMemo(
    () => runsQuery.data?.pages.flatMap((page) => page.runs) ?? [],
    [runsQuery.data],
  );
  // Every page recomputes the summary fresh; after a refetch page 0 is
  // refetched first (from the initial cursor) so it is always the newest.
  const summary: RunsSummary = runsQuery.data?.pages[0]?.summary ?? EMPTY_RUNS_SUMMARY;

  const toggleChecked = useCallback((id: string) => {
    setCheckedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);
  const clearSelection = useCallback(() => setCheckedIds(new Set()), []);

  const handleFilterChange = useCallback(<T,>(setter: (v: T) => void) => {
    return (v: T) => {
      setter(v);
      setCheckedIds(new Set());
    };
  }, []);

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <CollectionPageHeader icon={Server} title={t(($) => $.title)} description={t(($) => $.subtitle)} />
      <RunsHaltBlock wsId={wsId} />
      <RunsSummaryBar summary={summary} />
      <RunsFilters
        wsId={wsId}
        state={stateFilter}
        onStateChange={handleFilterChange(setStateFilter)}
        agentId={agentId}
        onAgentChange={handleFilterChange(setAgentId)}
        runtimeId={runtimeId}
        onRuntimeChange={handleFilterChange(setRuntimeId)}
        onIssueChange={handleFilterChange(setIssueId)}
      />
      <RunsList
        wsId={wsId}
        runs={runs}
        isLoading={runsQuery.isLoading}
        isError={runsQuery.isError}
        checkedIds={checkedIds}
        onToggleChecked={toggleChecked}
        hasNextPage={!!runsQuery.hasNextPage}
        isFetchingNextPage={runsQuery.isFetchingNextPage}
        onLoadMore={() => void runsQuery.fetchNextPage()}
        wsPaths={wsPaths}
      />
      {checkedIds.size > 0 ? (
        <RunsBulkBar wsId={wsId} ids={[...checkedIds]} onDone={clearSelection} />
      ) : null}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Halt block: the halted banner (everyone) with Lift the halt (owner/admin),
// or the Kill switch button (owner/admin only) when the fleet is not halted.
// ---------------------------------------------------------------------------

function RunsHaltBlock({ wsId }: { wsId: string }) {
  const { t } = useT("runs");
  const { t: tIssues } = useT("issues");
  const timeAgo = useTimeAgo();
  const { data: halt = EMPTY_RUN_HALT } = useQuery(runHaltOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { getMemberName } = useActorName();
  const currentUser = useAuthStore((s) => s.user);
  const setRunHalt = useSetRunHalt(wsId);
  const killSwitch = useKillSwitch(wsId);
  const [killOpen, setKillOpen] = useState(false);
  const [reason, setReason] = useState("");

  const canManage = useMemo(() => {
    if (!currentUser) return false;
    const role = members.find((m) => m.user_id === currentUser.id)?.role;
    return role === "owner" || role === "admin";
  }, [members, currentUser]);

  const lift = () => {
    setRunHalt.mutate(
      { halted: false, reason: "" },
      { onError: () => toast.error(tIssues(($) => $.approvals.run_halt_lift_failed)) },
    );
  };

  const confirmKillSwitch = () => {
    killSwitch.mutate(reason, {
      onSuccess: (data) => {
        toast.success(t(($) => $.halt.kill_switch_toast, { count: data.cancelled }));
        setKillOpen(false);
        setReason("");
      },
      onError: () => toast.error(t(($) => $.halt.kill_switch_failed)),
    });
  };

  if (halt.halted) {
    return (
      <div
        data-testid="runs-halt-banner"
        className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-destructive/40 bg-destructive/10 px-4 py-2 text-caption"
      >
        <div className="flex items-center gap-1.5 font-medium text-destructive">
          <ShieldOff className="size-3.5 shrink-0" aria-hidden="true" />
          {tIssues(($) => $.approvals.run_halt_title)}
        </div>
        <span className="text-muted-foreground">
          {tIssues(($) => $.approvals.run_halt_by, {
            who: halt.halted_by ? getMemberName(halt.halted_by) : tIssues(($) => $.approvals.run_halt_unknown_actor),
            when: halt.halted_at ? timeAgo(halt.halted_at) : "",
          })}
          {halt.reason ? ` · ${tIssues(($) => $.approvals.run_halt_reason_prefix, { reason: halt.reason })}` : ""}
        </span>
        {canManage ? (
          <Button size="sm" variant="outline" className="ml-auto" disabled={setRunHalt.isPending} onClick={lift}>
            {tIssues(($) => $.approvals.run_halt_lift)}
          </Button>
        ) : null}
      </div>
    );
  }

  if (!canManage) return null;

  return (
    <div className="flex shrink-0 items-center justify-end border-b px-4 py-2">
      <Button
        size="sm"
        variant="outline"
        className="text-destructive hover:text-destructive"
        onClick={() => setKillOpen(true)}
      >
        <ShieldOff className="size-3.5" aria-hidden="true" />
        {t(($) => $.halt.kill_switch)}
      </Button>
      <Dialog open={killOpen} onOpenChange={setKillOpen}>
        <DialogContent onClick={(e) => e.stopPropagation()}>
          <DialogHeader>
            <DialogTitle>{t(($) => $.halt.kill_switch_dialog_title)}</DialogTitle>
            <DialogDescription>{t(($) => $.halt.kill_switch_dialog_body)}</DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-1.5 py-2">
            <label htmlFor="kill-switch-reason" className="text-caption text-muted-foreground">
              {t(($) => $.halt.kill_switch_reason_label)}
            </label>
            <Input
              id="kill-switch-reason"
              autoFocus
              value={reason}
              disabled={killSwitch.isPending}
              onChange={(e) => setReason(e.target.value)}
              placeholder={t(($) => $.halt.kill_switch_reason_placeholder)}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" disabled={killSwitch.isPending} onClick={() => setKillOpen(false)}>
              {t(($) => $.halt.kill_switch_cancel)}
            </Button>
            <Button variant="destructive" disabled={killSwitch.isPending} onClick={confirmKillSwitch}>
              {killSwitch.isPending ? <Loader2 className="size-3.5 animate-spin" aria-hidden="true" /> : null}
              {t(($) => $.halt.kill_switch_confirm)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Summary bar
// ---------------------------------------------------------------------------

function RunsSummaryBar({ summary }: { summary: RunsSummary }) {
  const { t } = useT("runs");
  const cards: { label: string; value: string }[] = [
    { label: t(($) => $.summary.queued), value: String(summary.queued) },
    { label: t(($) => $.summary.running), value: String(summary.running) },
    { label: t(($) => $.summary.blocked), value: String(summary.blocked) },
    { label: t(($) => $.summary.completed_since), value: String(summary.completed_since) },
    { label: t(($) => $.summary.failed_since), value: String(summary.failed_since) },
    { label: t(($) => $.summary.cancelled_since), value: String(summary.cancelled_since) },
    // Priced like budget settlement; runs that left no priceable usage are
    // not in the figure, and the tile says how many instead of hiding them.
    {
      label: t(($) => $.summary.cost_since),
      value: summary.cost_unknown_since > 0
        ? t(($) => $.summary.cost_since_partial, { cost: formatUsd(runCostUsd(summary.cost_since_usd_ticks)), count: summary.cost_unknown_since })
        : formatUsd(runCostUsd(summary.cost_since_usd_ticks)),
    },
  ];
  return (
    <div className="flex shrink-0 flex-wrap items-center gap-x-5 gap-y-1 border-b px-4 py-2 text-caption">
      {cards.map((card) => (
        <span key={card.label} className="flex items-baseline gap-1.5 text-muted-foreground">
          {card.label}
          <strong className="font-mono tabular-nums text-foreground">{card.value}</strong>
        </span>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Filters
// ---------------------------------------------------------------------------

function RunsFilters({
  wsId,
  state,
  onStateChange,
  agentId,
  onAgentChange,
  runtimeId,
  onRuntimeChange,
  onIssueChange,
}: {
  wsId: string;
  state: StateFilter;
  onStateChange: (v: StateFilter) => void;
  agentId: string | undefined;
  onAgentChange: (v: string | undefined) => void;
  runtimeId: string | undefined;
  onRuntimeChange: (v: string | undefined) => void;
  onIssueChange: (v: string | undefined) => void;
}) {
  const { t } = useT("runs");
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const [issueQuery, setIssueQuery] = useState("");

  const resolveIssue = useCallback(async () => {
    const q = issueQuery.trim();
    if (!q) {
      onIssueChange(undefined);
      return;
    }
    if (ISSUE_UUID_RE.test(q)) {
      onIssueChange(q);
      return;
    }
    try {
      const res = await api.searchIssues({ q, limit: 1 });
      onIssueChange(res.issues[0]?.id);
    } catch {
      onIssueChange(undefined);
    }
  }, [issueQuery, onIssueChange]);

  return (
    <div className="flex shrink-0 flex-wrap items-center gap-2 border-b px-4 py-2">
      <SegmentedToggle
        value={state}
        onChange={onStateChange}
        options={[
          ["in_flight", t(($) => $.filters.state_in_flight)],
          ["finished", t(($) => $.filters.state_finished)],
          ["all", t(($) => $.filters.state_all)],
        ]}
      />
      <Select
        items={[
          { value: "all", label: t(($) => $.filters.agent_all) },
          ...agents.map((agent) => ({ value: agent.id, label: agent.name })),
        ]}
        value={agentId ?? "all"}
        onValueChange={(v) => v && onAgentChange(v === "all" ? undefined : v)}
      >
        <SelectTrigger className="h-8 w-40 text-caption">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">{t(($) => $.filters.agent_all)}</SelectItem>
          {agents.map((agent) => (
            <SelectItem key={agent.id} value={agent.id}>
              {agent.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Select
        items={[
          { value: "all", label: t(($) => $.filters.runtime_all) },
          ...runtimes.map((runtime) => ({ value: runtime.id, label: runtimeDisplayLabel(runtime) })),
        ]}
        value={runtimeId ?? "all"}
        onValueChange={(v) => v && onRuntimeChange(v === "all" ? undefined : v)}
      >
        <SelectTrigger className="h-8 w-44 text-caption">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">{t(($) => $.filters.runtime_all)}</SelectItem>
          {runtimes.map((runtime) => (
            <SelectItem key={runtime.id} value={runtime.id}>
              {runtimeDisplayLabel(runtime)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Input
        className="h-8 w-48 text-caption"
        placeholder={t(($) => $.filters.issue_placeholder)}
        value={issueQuery}
        onChange={(e) => setIssueQuery(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") void resolveIssue();
        }}
        onBlur={() => void resolveIssue()}
      />
    </div>
  );
}

// ---------------------------------------------------------------------------
// List + rows
// ---------------------------------------------------------------------------

function RunsList({
  wsId,
  runs,
  isLoading,
  isError,
  checkedIds,
  onToggleChecked,
  hasNextPage,
  isFetchingNextPage,
  onLoadMore,
  wsPaths,
}: {
  wsId: string;
  runs: Run[];
  isLoading: boolean;
  isError: boolean;
  checkedIds: Set<string>;
  onToggleChecked: (id: string) => void;
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  onLoadMore: () => void;
  wsPaths: ReturnType<typeof useWorkspacePaths>;
}) {
  const { t } = useT("runs");

  if (isLoading) {
    return (
      <div className="flex w-full flex-1 flex-col gap-2 overflow-y-auto p-4" aria-busy="true">
        <span className="sr-only">{t(($) => $.list.loading)}</span>
        {[0, 1, 2, 3].map((i) => (
          <Skeleton key={i} className="h-12 w-full rounded-lg" />
        ))}
      </div>
    );
  }

  if (isError) {
    return (
      <div className="flex w-full flex-1 items-center justify-center">
        <CollectionPageState icon={Server} title={t(($) => $.list.load_error)} tone="destructive" role="alert" />
      </div>
    );
  }

  if (runs.length === 0) {
    return (
      <div className="flex w-full flex-1 items-center justify-center">
        <CollectionPageState
          icon={Server}
          title={t(($) => $.list.empty_title)}
          description={t(($) => $.list.empty_description)}
        />
      </div>
    );
  }

  return (
    <ul className="flex w-full min-w-0 flex-1 flex-col gap-1 overflow-y-auto p-2">
      {runs.map((run) => (
        <RunRow
          key={run.id}
          wsId={wsId}
          run={run}
          checked={checkedIds.has(run.id)}
          onToggleChecked={() => onToggleChecked(run.id)}
          wsPaths={wsPaths}
        />
      ))}
      {hasNextPage ? (
        <li>
          <Button
            type="button"
            size="sm"
            variant="ghost"
            className="w-full"
            disabled={isFetchingNextPage}
            onClick={onLoadMore}
          >
            {isFetchingNextPage ? <Loader2 className="size-3.5 animate-spin" aria-hidden="true" /> : null}
            {t(($) => $.list.load_more)}
          </Button>
        </li>
      ) : null}
    </ul>
  );
}

function RunRow({
  wsId,
  run,
  checked,
  onToggleChecked,
  wsPaths,
}: {
  wsId: string;
  run: Run;
  checked: boolean;
  onToggleChecked: () => void;
  wsPaths: ReturnType<typeof useWorkspacePaths>;
}) {
  const { t } = useT("runs");
  const cancelRuns = useCancelRuns(wsId);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const status = run.status as TaskStatus;
  const tone = STATUS_TONE[status] ?? STATUS_TONE.queued;
  const label = useStatusLabel(run.status);
  const silent = isRunSilent(run);
  const issueHref = run.issue ? `${wsPaths.issueDetail(run.issue.id)}${blockerHash(run.blocked_on) ?? ""}` : null;
  const showTranscript = run.status !== "queued" && run.status !== "waiting_local_directory";
  // Run is a strict superset of AgentTask's fields (see fleet-schemas.ts):
  // the one narrowing mismatch is `kind`, which AgentTaskSchema keeps as an
  // open string on the wire (a server may add a kind this build predates).
  const task = run as unknown as AgentTask;

  // A follow-up (réveil programmé) shows here as a run blocked on "deferred",
  // and that word says nothing. The row already carries the wake-up's own
  // trigger summary ("Follow-up: <note>") — read it instead when there is one.
  const blockedLabel =
    run.blocked_on?.kind === "deferred" && run.trigger_summary
      ? run.trigger_summary
      : run.blocked_on?.summary ?? "";

  const handleCancel = async () => {
    try {
      const results = await cancelRuns.mutateAsync([run.id]);
      reportCancelOutcomes(results.results, t);
    } catch {
      toast.error(t(($) => $.list.cancel_failed));
    }
  };

  return (
    <li>
      <div className="group flex w-full items-center gap-2 rounded-lg border border-transparent px-2 py-1.5 transition-colors hover:bg-accent/60">
        <span
          className={cn(
            "-m-1 flex shrink-0 items-center p-1",
            checked ? "" : "opacity-0 transition-opacity group-hover:opacity-100",
          )}
        >
          <Checkbox checked={checked} onCheckedChange={onToggleChecked} aria-label={run.agent_name} />
        </span>

        <ActorAvatar actorType="agent" actorId={run.agent_id} name={run.agent_name} size="xs" enableHoverCard />
        <span title={run.agent_name} className="w-32 shrink-0 truncate text-caption">{run.agent_name}</span>

        {run.issue && issueHref ? (
          <AppLink
            href={issueHref}
            title={`${run.issue.identifier} · ${run.issue.title}`}
            className="min-w-0 flex-1 truncate text-caption text-info hover:underline"
          >
            {run.issue.identifier} · {run.issue.title}
          </AppLink>
        ) : (
          <span className="min-w-0 flex-1 truncate text-caption text-muted-foreground">—</span>
        )}

        <span className="flex w-28 shrink-0 items-center gap-1">
          <TaskStatusIcon status={run.status} />
          <span className={cn("truncate text-caption", tone)}>{label}</span>
        </span>

        {silent ? (
          <Tooltip>
            <TooltipTrigger
              render={<span data-testid="run-silent" className="shrink-0 text-caption text-warning" />}
            >
              {t(($) => $.row.silent)}
            </TooltipTrigger>
            <TooltipContent>
              {t(($) => $.row.silent_tooltip, { duration: formatDurationMs(run.silence_ms) })}
            </TooltipContent>
          </Tooltip>
        ) : null}

        {run.blocked_on ? (
          issueHref ? (
            <AppLink
              href={issueHref}
              className="max-w-40 shrink-0 truncate rounded bg-warning/10 px-1.5 py-0.5 text-caption text-warning hover:underline"
              title={`${t(($) => $.row.blocked_on)}: ${blockedLabel}`}
            >
              {blockedLabel}
            </AppLink>
          ) : (
            <span
              className="max-w-40 shrink-0 truncate rounded bg-warning/10 px-1.5 py-0.5 text-caption text-warning"
              title={`${t(($) => $.row.blocked_on)}: ${blockedLabel}`}
            >
              {blockedLabel}
            </span>
          )
        ) : null}

        <span className="w-16 shrink-0 truncate text-right font-mono text-caption tabular-nums text-muted-foreground">
          {formatDurationMs(run.duration_ms)}
        </span>
        <span className="w-16 shrink-0 truncate text-right font-mono text-caption tabular-nums text-muted-foreground">
          {/* No priceable usage reads "unknown", never "$0.00" beside an issue page saying unavailable. */}
          {runCostKnown(run) ? formatUsd(runCostUsd(run.cost_usd_ticks)) : (
            <span title={t(($) => $.row.cost_unknown_tooltip)}>{t(($) => $.row.cost_unknown)}</span>
          )}
        </span>

        <div className="flex shrink-0 items-center gap-0.5">
          {showTranscript ? (
            <TranscriptButton
              task={task}
              agentName={run.agent_name}
              isLive={run.status === "running"}
              title={t(($) => $.row.transcript_tooltip)}
            />
          ) : null}
          <ReplayButton task={task} />
          {isSettledRunStatus(run.status) ? null : (
          <Tooltip>
            <TooltipTrigger
              render={
                <button
                  type="button"
                  data-testid="run-cancel"
                  aria-label={t(($) => $.row.cancel_aria)}
                  disabled={cancelRuns.isPending}
                  onClick={() => setConfirmOpen(true)}
                />
              }
              className="flex items-center justify-center rounded p-1 text-destructive transition-colors hover:bg-destructive/10 disabled:cursor-not-allowed disabled:opacity-50"
            >
              <Ban className="h-3.5 w-3.5" aria-hidden="true" />
            </TooltipTrigger>
            <TooltipContent>{t(($) => $.row.cancel_tooltip)}</TooltipContent>
          </Tooltip>
          )}
        </div>
      </div>
      <RunCancelConfirmDialog
        open={confirmOpen}
        count={1}
        onOpenChange={setConfirmOpen}
        onConfirm={() => void handleCancel()}
      />
    </li>
  );
}

// ---------------------------------------------------------------------------
// Cancel: shared confirm dialog + outcome toast for one row or a selection.
// ---------------------------------------------------------------------------

function RunCancelConfirmDialog({
  open,
  count,
  onOpenChange,
  onConfirm,
}: {
  open: boolean;
  count: number;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
}) {
  const { t } = useT("runs");
  if (!open) return null;
  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent onClick={(e) => e.stopPropagation()}>
        <DialogHeader>
          <DialogTitle>{t(($) => $.cancel_dialog.title, { count })}</DialogTitle>
          <DialogDescription>{t(($) => $.cancel_dialog.body)}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t(($) => $.cancel_dialog.keep)}
          </Button>
          <Button
            variant="destructive"
            onClick={() => {
              onOpenChange(false);
              onConfirm();
            }}
          >
            {t(($) => $.cancel_dialog.confirm)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function reportCancelOutcomes(results: RunCancelOutcome[], t: ReturnType<typeof useT<"runs">>["t"]) {
  const cancelled = results.filter((r) => r.outcome === "cancelled").length;
  const alreadyOver = results.filter((r) => r.outcome === "already_over").length;
  const failed = results.length - cancelled - alreadyOver;
  if (failed > 0) {
    toast.error(t(($) => $.cancel_dialog.toast, { cancelled, alreadyOver, failed }));
  } else {
    toast.success(t(($) => $.cancel_dialog.toast, { cancelled, alreadyOver, failed }));
  }
}

// ---------------------------------------------------------------------------
// Bulk bar
// ---------------------------------------------------------------------------

function RunsBulkBar({ wsId, ids, onDone }: { wsId: string; ids: string[]; onDone: () => void }) {
  const { t } = useT("runs");
  const cancelRuns = useCancelRuns(wsId);
  const [confirmOpen, setConfirmOpen] = useState(false);

  const handleCancel = async () => {
    try {
      const res = await cancelRuns.mutateAsync(ids);
      reportCancelOutcomes(res.results, t);
      onDone();
    } catch {
      toast.error(t(($) => $.list.cancel_failed));
    }
  };

  return (
    <div className="flex shrink-0 items-center gap-3 border-t bg-accent/40 px-4 py-2 text-caption">
      <span className="text-muted-foreground">{t(($) => $.bulk.selected_count, { count: ids.length })}</span>
      <Button size="sm" variant="destructive" disabled={cancelRuns.isPending} onClick={() => setConfirmOpen(true)}>
        {cancelRuns.isPending ? <Loader2 className="size-3.5 animate-spin" aria-hidden="true" /> : null}
        {t(($) => $.bulk.cancel_selected)}
      </Button>
      <RunCancelConfirmDialog
        open={confirmOpen}
        count={ids.length}
        onOpenChange={setConfirmOpen}
        onConfirm={() => void handleCancel()}
      />
    </div>
  );
}
