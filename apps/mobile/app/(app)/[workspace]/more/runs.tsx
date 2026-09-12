/**
 * Runs fleet screen (OS plan, chantier 4) — mobile parity with
 * `apps/docs/content/docs/runs.mdx` / `server/internal/handler/runs.go`.
 * No web/desktop page exists yet to mirror pixel-for-pixel (only the
 * backend + docs landed so far), so this follows the contract directly:
 * summary strip, halt banner, segmented state filter, cursor-paginated
 * list, cancel and kill switch.
 *
 * Realtime mounts here (not in `_layout.tsx`'s `<RealtimeSubscriptions />`)
 * because, unlike approvals/triage/postmortems, nothing else in the app
 * shows a live Runs count — see `use-runs-realtime.ts` for the full
 * reasoning.
 *
 * Role gating (owner/admin for Lift + Kill switch) mirrors web's
 * `RunHaltBanner` (`packages/views/approvals/run-halt-banner.tsx`):
 * derive the caller's role from the member list rather than trusting a
 * client-side flag.
 */
import { useMemo, useState } from "react";
import { ActivityIndicator, Alert, FlatList, Pressable, View } from "react-native";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { router } from "expo-router";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { RunRow } from "@/components/issue/run-row";
import {
  flattenRunPages,
  runsListOptions,
  type RunsFilterState,
} from "@/data/queries/runs";
import { memberListOptions } from "@/data/queries/members";
import { useCancelRuns, useKillSwitch, useLiftRunHalt } from "@/data/mutations/runs";
import { useRunsRealtime } from "@/data/realtime/use-runs-realtime";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { EMPTY_RUNS_SUMMARY, type RunsSummary } from "@/data/schemas";
import { costTodayLabel, isRunSilent, runCostKnown } from "@/lib/runs-display";
import { timeAgo } from "@/lib/time-ago";

const FILTERS: { state: RunsFilterState; label: string }[] = [
  { state: "active", label: "In flight" },
  { state: "terminal", label: "Finished" },
  { state: "all", label: "All" },
];

export default function RunsPage() {
  useRunsRealtime();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const user = useAuthStore((s) => s.user);
  const [filter, setFilter] = useState<RunsFilterState>("active");

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const canManage = useMemo(() => {
    const role = members.find((m) => m.user_id === user?.id)?.role;
    return role === "owner" || role === "admin";
  }, [members, user?.id]);

  const {
    data,
    isLoading,
    error,
    refetch,
    isRefetching,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useInfiniteQuery(runsListOptions(wsId, filter));

  const runs = useMemo(() => flattenRunPages(data?.pages), [data?.pages]);
  const summary: RunsSummary = data?.pages[0]?.summary ?? EMPTY_RUNS_SUMMARY;

  const cancelRuns = useCancelRuns(wsId);
  const killSwitch = useKillSwitch(wsId);
  const liftHalt = useLiftRunHalt(wsId);

  const openIssue = (issueId: string) => {
    if (!wsSlug) return;
    router.push({
      pathname: "/[workspace]/issue/[id]",
      params: { workspace: wsSlug, id: issueId },
    });
  };

  const onCancelRun = (taskId: string) => {
    cancelRuns.mutate([taskId], {
      onError: (err) =>
        Alert.alert(
          "Could not cancel",
          err instanceof Error ? err.message : "unknown error",
        ),
    });
  };

  const confirmKillSwitch = () => {
    // Alert.prompt's array-of-buttons overload combines the text input and
    // the destructive confirm in one native modal (iOS-native > RNR > ask
    // waterfall, apps/mobile/CLAUDE.md) — no custom confirm sheet needed.
    Alert.prompt(
      "Kill switch",
      "Halts the fleet — no run is claimed, every gated action is refused — then cancels every run in progress. Reason:",
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Halt & cancel all",
          style: "destructive",
          onPress: (reason?: string) =>
            killSwitch.mutate(reason ?? "", {
              onSuccess: (res) =>
                // Mobile has no toast system (components/issue/comment-card.tsx)
                // — a native alert is the closest equivalent to "toast with
                // the cancelled count".
                Alert.alert(
                  "Fleet halted",
                  `Cancelled ${res.cancelled} run${res.cancelled === 1 ? "" : "s"}.`,
                ),
              onError: (err) =>
                Alert.alert(
                  "Kill switch failed",
                  err instanceof Error ? err.message : "unknown error",
                ),
            }),
        },
      ],
      "plain-text",
    );
  };

  const onLiftHalt = () => {
    liftHalt.mutate(undefined, {
      onError: (err) =>
        Alert.alert(
          "Could not lift the halt",
          err instanceof Error ? err.message : "unknown error",
        ),
    });
  };

  if (isLoading) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator />
      </View>
    );
  }

  if (error) {
    return (
      <View className="flex-1 bg-background px-4 gap-3 pt-4">
        <Text className="text-sm text-destructive">
          Could not load runs: {error instanceof Error ? error.message : "unknown error"}
        </Text>
        <Button variant="outline" onPress={() => refetch()}>
          <Text>Retry</Text>
        </Button>
      </View>
    );
  }

  return (
    <View className="flex-1 bg-background">
      {summary.run_halt.halted ? (
        <HaltBanner
          summary={summary}
          canLift={canManage}
          onLift={onLiftHalt}
          lifting={liftHalt.isPending}
        />
      ) : null}

      <SummaryStrip
        summary={summary}
        canManage={canManage}
        onKillSwitch={confirmKillSwitch}
        killSwitchPending={killSwitch.isPending}
      />

      <View className="flex-row items-center gap-1 px-4 pb-2">
        {FILTERS.map((f) => {
          const active = f.state === filter;
          return (
            <Button
              key={f.state}
              variant="outline"
              size="sm"
              onPress={() => setFilter(f.state)}
              className={active ? "bg-accent" : ""}
              accessibilityState={{ selected: active }}
            >
              <Text className={active ? "text-accent-foreground" : "text-muted-foreground"}>
                {f.label}
              </Text>
            </Button>
          );
        })}
      </View>

      <FlatList
        data={runs}
        keyExtractor={(run) => run.id}
        ItemSeparatorComponent={() => <View className="h-px bg-border ml-4" />}
        contentContainerClassName="pb-6"
        ListEmptyComponent={
          <Text className="px-6 py-12 text-center text-sm text-muted-foreground">
            {filter === "active"
              ? "No runs in flight. Every agent is idle."
              : filter === "terminal"
                ? "No finished runs yet."
                : "No runs in this workspace yet."}
          </Text>
        }
        ListFooterComponent={
          isFetchingNextPage ? (
            <View className="py-4">
              <ActivityIndicator />
            </View>
          ) : null
        }
        onEndReachedThreshold={0.4}
        onEndReached={() => {
          if (hasNextPage && !isFetchingNextPage) fetchNextPage();
        }}
        refreshing={isRefetching}
        onRefresh={refetch}
        renderItem={({ item }) => (
          <View className="px-4">
            <RunRow
              task={item}
              issueId={item.issue?.id ?? item.issue_id}
              agentName={item.agent_name}
              issueRef={item.issue}
              costUsdTicks={runCostKnown(item) ? item.cost_usd_ticks : undefined}
              blockedOn={item.blocked_on}
              silent={isRunSilent(item)}
              onPressBlocker={
                item.issue?.id ? () => openIssue(item.issue!.id) : undefined
              }
              onCancel={onCancelRun}
            />
          </View>
        )}
      />
    </View>
  );
}

function HaltBanner({
  summary,
  canLift,
  onLift,
  lifting,
}: {
  summary: RunsSummary;
  canLift: boolean;
  onLift: () => void;
  lifting: boolean;
}) {
  const halt = summary.run_halt;
  return (
    <View className="flex-row flex-wrap items-center gap-x-3 gap-y-1 border-b border-destructive/40 bg-destructive/10 px-4 py-2">
      <Text className="text-xs font-medium text-destructive">Fleet halted</Text>
      <Text className="flex-1 text-xs text-muted-foreground" numberOfLines={2}>
        {halt.halted_at ? timeAgo(halt.halted_at) : ""}
        {halt.reason ? ` · ${halt.reason}` : ""}
      </Text>
      {canLift ? (
        <Pressable
          onPress={onLift}
          disabled={lifting}
          className="rounded-md border border-border bg-background px-2.5 py-1 active:bg-accent"
        >
          <Text className="text-xs font-medium text-foreground">Lift</Text>
        </Pressable>
      ) : null}
    </View>
  );
}

function SummaryStrip({
  summary,
  canManage,
  onKillSwitch,
  killSwitchPending,
}: {
  summary: RunsSummary;
  canManage: boolean;
  onKillSwitch: () => void;
  killSwitchPending: boolean;
}) {
  const finishedToday = summary.completed_since + summary.failed_since + summary.cancelled_since;
  return (
    <View className="flex-row items-start gap-4 px-4 pt-3 pb-2">
      <View className="flex-1 flex-row flex-wrap gap-x-4 gap-y-1">
        <Stat label="Queued" value={summary.queued} />
        <Stat label="Running" value={summary.running} />
        <Stat label="Blocked" value={summary.blocked} />
        <Stat label="Finished today" value={finishedToday} />
        <Stat label="Cost today" value={costTodayLabel(summary)} />
      </View>
      {canManage ? (
        <Pressable
          onPress={onKillSwitch}
          disabled={killSwitchPending}
          className="rounded-md bg-destructive px-2.5 py-1.5 active:bg-destructive/90"
        >
          <Text className="text-xs font-medium text-destructive-foreground">Kill switch</Text>
        </Pressable>
      ) : null}
    </View>
  );
}

function Stat({ label, value }: { label: string; value: number | string }) {
  return (
    <View className="gap-0.5">
      <Text className="text-base font-semibold text-foreground">{value}</Text>
      <Text className="text-[11px] text-muted-foreground">{label}</Text>
    </View>
  );
}
