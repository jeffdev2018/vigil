/**
 * Workspace doctrine — read, review, resolve (OS plan, chantier 22).
 *
 * The doctrine is the one governing document the owners write for every
 * agent of the workspace. Parity target is
 * `server/internal/handler/workspace_doctrine.go`: no packages/views page
 * exists yet, so the enums, the counts and the permissions come from the
 * handler.
 *
 * What mobile deliberately does NOT do: write the doctrine. Publishing
 * (PUT /api/workspace/doctrine) and restoring an old revision are
 * web/desktop actions — a 32 000-byte governing document is not edited on a
 * phone. The phone reads it, reviews a proposal someone else wrote, and
 * clears the reports agents filed against it.
 *
 * Permissions mirror the handler, not the feel: manager actions appear only
 * when `can_publish` is true, and Approve/Reject only when
 * `canReviewDoctrineVersion` agrees (see lib/doctrine-display.ts for the
 * self-review rule).
 */
import { useMemo, useState } from "react";
import { ActivityIndicator, Alert, FlatList, Pressable, View } from "react-native";
import { router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import {
  doctrineOptions,
  doctrineReportsOptions,
} from "@/data/queries/doctrine";
import { memberListOptions } from "@/data/queries/members";
import {
  useResolveDoctrineReport,
  useReviewDoctrineVersion,
} from "@/data/mutations/doctrine";
import type { DoctrineReport, DoctrineVersion } from "@/data/schemas";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useActorLookup } from "@/data/use-actor-name";
import {
  canReviewDoctrineVersion,
  countOtherManagers,
  doctrineReportKindLabel,
  doctrineReportStatusLabel,
} from "@/lib/doctrine-display";
import { timeAgo } from "@/lib/time-ago";
import { cn } from "@/lib/utils";

type ReportFilter = "open" | "resolved";

/**
 * The server filters by a single status (open | acknowledged | dismissed |
 * all), so "resolved" is `all` minus the open ones — one extra cache entry
 * instead of two requests. `open_reports` on the doctrine payload stays the
 * authoritative count for the badge, so mobile and web agree on the number
 * even while this list is stale.
 */
function apiStatusFor(filter: ReportFilter): "open" | "all" {
  return filter === "open" ? "open" : "all";
}

/** Optional review/resolution note through the iOS native prompt (the
 *  waterfall in apps/mobile/CLAUDE.md: Alert.prompt before any hand-rolled
 *  input sheet). Cancel aborts, an empty note is allowed — the server
 *  accepts one. */
function promptForNote(
  title: string,
  confirmLabel: string,
  destructive: boolean,
  onConfirm: (note: string) => void,
) {
  Alert.prompt(
    title,
    "Add a note (optional)",
    [
      { text: "Cancel", style: "cancel" },
      {
        text: confirmLabel,
        style: destructive ? "destructive" : "default",
        onPress: (note?: string) => onConfirm(note ?? ""),
      },
    ],
    "plain-text",
  );
}

export default function DoctrineScreen() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const [filter, setFilter] = useState<ReportFilter>("open");

  const {
    data: doctrine,
    isLoading,
    error,
    refetch,
    isRefetching,
  } = useQuery(doctrineOptions(wsId));
  const { data: reportsResponse } = useQuery(
    doctrineReportsOptions(wsId, apiStatusFor(filter)),
  );

  const reports = useMemo(() => {
    const all = reportsResponse?.reports ?? [];
    return filter === "open" ? all : all.filter((r) => r.status !== "open");
  }, [reportsResponse, filter]);

  if (isLoading) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator />
      </View>
    );
  }

  if (error) {
    return (
      <View className="flex-1 gap-3 bg-background px-4 pt-4">
        <Text className="text-sm text-destructive">
          Could not load the doctrine:{" "}
          {error instanceof Error ? error.message : "unknown error"}
        </Text>
        <Button variant="outline" onPress={() => refetch()}>
          <Text>Retry</Text>
        </Button>
      </View>
    );
  }

  return (
    <FlatList
      className="flex-1 bg-background"
      data={reports}
      keyExtractor={(item) => item.id}
      ItemSeparatorComponent={() => <View className="h-px bg-border ml-4" />}
      contentContainerClassName="pb-10"
      refreshing={isRefetching}
      onRefresh={refetch}
      ListHeaderComponent={
        <View>
          <DoctrineBody
            content={doctrine?.content ?? ""}
            revision={doctrine?.revision ?? 0}
            updatedAt={doctrine?.updated_at ?? null}
            updatedBy={doctrine?.updated_by ?? null}
          />
          {doctrine?.pending ? (
            <PendingProposalCard
              version={doctrine.pending}
              canPublish={doctrine.can_publish === true}
            />
          ) : null}
          <View className="flex-row items-center gap-1 px-4 pb-2 pt-5">
            <Text className="flex-1 text-base font-semibold text-foreground">
              Reports
              {(doctrine?.open_reports ?? 0) > 0
                ? ` · ${doctrine?.open_reports} open`
                : ""}
            </Text>
          </View>
          {/* Pill row rather than a UISegmentedControl: every other filtered
              list on mobile (Postmortems, Triage) uses this shape, and
              apps/mobile/CLAUDE.md puts "existing pattern first" ahead of
              the native waterfall. */}
          <View className="flex-row items-center gap-1 px-4 pb-3">
            {(["open", "resolved"] as ReportFilter[]).map((f) => {
              const active = f === filter;
              return (
                <Button
                  key={f}
                  variant="outline"
                  size="sm"
                  onPress={() => setFilter(f)}
                  className={active ? "bg-accent" : ""}
                  accessibilityState={{ selected: active }}
                >
                  <Text
                    className={
                      active
                        ? "text-accent-foreground"
                        : "text-muted-foreground"
                    }
                  >
                    {f === "open" ? "Open" : "Resolved"}
                  </Text>
                </Button>
              );
            })}
          </View>
        </View>
      }
      ListEmptyComponent={
        <Text className="px-6 py-10 text-center text-sm text-muted-foreground">
          {filter === "open"
            ? "No agent has reported a problem with the doctrine."
            : "No report has been resolved yet."}
        </Text>
      }
      renderItem={({ item }) => (
        <ReportRow
          report={item}
          canResolve={doctrine?.can_publish === true}
        />
      )}
    />
  );
}

function DoctrineBody({
  content,
  revision,
  updatedAt,
  updatedBy,
}: {
  content: string;
  revision: number;
  updatedAt: string | null;
  updatedBy: string | null;
}) {
  const { getName } = useActorLookup();
  const meta = [
    `Revision ${revision}`,
    updatedAt ? timeAgo(updatedAt) : null,
    updatedBy ? getName("member", updatedBy) : null,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <View className="gap-2 px-4 pt-4">
      <Text className="text-xs text-muted-foreground">{meta}</Text>
      {content.trim() ? (
        <View className="rounded-md bg-code-surface p-3">
          {/* The doctrine is plain text with meaningful line breaks; a mono
              face keeps a numbered rule list aligned the way its author saw
              it on web. */}
          <Text className="font-mono text-xs leading-5 text-foreground">
            {content}
          </Text>
        </View>
      ) : (
        <Text className="py-6 text-center text-sm text-muted-foreground">
          This workspace has no doctrine yet. Owners write it on the web or
          desktop app.
        </Text>
      )}
    </View>
  );
}

/**
 * The proposal waiting on a second reviewer. Mobile shows it to every
 * member (knowing a change is queued is not privileged) but only offers
 * Approve/Reject to a manager who is not its author.
 */
function PendingProposalCard({
  version,
  canPublish,
}: {
  version: DoctrineVersion;
  canPublish: boolean;
}) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { getName } = useActorLookup();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const review = useReviewDoctrineVersion();

  const canReview = canReviewDoctrineVersion({
    canPublish,
    status: version.status,
    authorId: version.author_id,
    userId,
    otherManagerCount: countOtherManagers(members, userId),
  });

  const author = version.author_id
    ? getName("member", version.author_id)
    : "Someone";

  return (
    <View className="mx-4 mt-4 gap-2 rounded-md border border-warning/50 bg-warning/10 p-3">
      <Text className="text-sm font-medium text-foreground">
        {author} proposed a change
      </Text>
      <Text className="text-xs text-muted-foreground">
        {[timeAgo(version.created_at), `${version.bytes} bytes`]
          .filter(Boolean)
          .join(" · ")}
      </Text>
      {version.note ? (
        <Text className="text-sm leading-5 text-foreground">
          {version.note}
        </Text>
      ) : null}
      <Button
        variant="outline"
        size="sm"
        onPress={() =>
          wsSlug &&
          router.push(`/${wsSlug}/more/doctrine-version/${version.id}`)
        }
      >
        <Text>View changes</Text>
      </Button>
      {canReview ? (
        <View className="flex-row gap-2">
          <Button
            className="flex-1"
            size="sm"
            disabled={review.isPending}
            onPress={() =>
              promptForNote("Approve the proposal", "Approve", false, (note) =>
                review.mutate({
                  id: version.id,
                  decision: "approve",
                  note,
                }),
              )
            }
          >
            <Text>Approve</Text>
          </Button>
          <Button
            className="flex-1"
            size="sm"
            variant="destructive"
            disabled={review.isPending}
            onPress={() =>
              promptForNote("Reject the proposal", "Reject", true, (note) =>
                review.mutate({ id: version.id, decision: "reject", note }),
              )
            }
          >
            <Text>Reject</Text>
          </Button>
        </View>
      ) : (
        <Text className="text-xs text-muted-foreground">
          Awaiting review by another owner or admin.
        </Text>
      )}
    </View>
  );
}

/** Kind tone: a conflict blocks the agent, a refusal is a deliberate stop,
 *  an ambiguity is a request for wording. Unknown kinds fall back to the
 *  neutral chip rather than disappearing. */
function kindChipClass(kind: string): string {
  switch (kind) {
    case "conflict":
      return "bg-destructive/10";
    case "refusal":
      return "bg-warning/15";
    default:
      return "bg-secondary";
  }
}

function ReportRow({
  report,
  canResolve,
}: {
  report: DoctrineReport;
  canResolve: boolean;
}) {
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { getName } = useActorLookup();
  const resolve = useResolveDoctrineReport();

  const reporter = getName(
    report.reporter_type === "agent" ? "agent" : "member",
    report.reporter_id,
  );
  const meta = [
    `Revision ${report.doctrine_revision}`,
    reporter,
    report.created_at ? timeAgo(report.created_at) : null,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <View className="gap-2 px-4 py-3">
      <View className="flex-row items-center gap-1.5">
        <View className={cn("rounded px-1.5 py-0.5", kindChipClass(report.kind))}>
          <Text className="text-xs text-foreground">
            {doctrineReportKindLabel(report.kind)}
          </Text>
        </View>
        {report.status !== "open" ? (
          <View className="rounded bg-secondary px-1.5 py-0.5">
            <Text className="text-xs text-muted-foreground">
              {doctrineReportStatusLabel(report.status)}
            </Text>
          </View>
        ) : null}
      </View>

      <Text className="text-sm leading-5 text-foreground">
        {report.summary}
      </Text>

      {report.passage ? (
        <View className="rounded-md bg-code-surface p-2">
          <Text
            className="font-mono text-xs leading-5 text-muted-foreground"
            numberOfLines={4}
          >
            {report.passage}
          </Text>
        </View>
      ) : null}

      <Text className="text-xs text-muted-foreground">{meta}</Text>

      {report.resolution_note ? (
        <Text className="text-xs leading-5 text-muted-foreground">
          {report.resolution_note}
        </Text>
      ) : null}

      {report.issue_id && wsSlug ? (
        <Pressable
          onPress={() => router.push(`/${wsSlug}/issue/${report.issue_id}`)}
          accessibilityLabel="Open the issue this report came from"
        >
          <Text className="text-xs text-brand">View issue</Text>
        </Pressable>
      ) : null}

      {canResolve && report.status === "open" ? (
        <View className="flex-row gap-2">
          <Button
            className="flex-1"
            variant="outline"
            size="sm"
            disabled={resolve.isPending}
            onPress={() =>
              promptForNote(
                "Acknowledge the report",
                "Acknowledge",
                false,
                (note) =>
                  resolve.mutate({
                    id: report.id,
                    resolution: "acknowledge",
                    note,
                  }),
              )
            }
          >
            <Text>Acknowledge</Text>
          </Button>
          <Button
            className="flex-1"
            variant="outline"
            size="sm"
            disabled={resolve.isPending}
            onPress={() =>
              promptForNote("Dismiss the report", "Dismiss", true, (note) =>
                resolve.mutate({
                  id: report.id,
                  resolution: "dismiss",
                  note,
                }),
              )
            }
          >
            <Text>Dismiss</Text>
          </Button>
        </View>
      ) : null}
    </View>
  );
}
