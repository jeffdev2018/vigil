/**
 * Triage item detail — mobile counterpart of web's detail aside in
 * `packages/views/triage/components/triage-page.tsx`.
 *
 * Source of truth is the triage list cache, not a fetch: the backend has no
 * `GET /api/triage/items/{id}` route (see `r.Route("/api/triage", …)` in
 * server/cmd/server/router.go), so web reads the selected item out of the
 * same infinite query that painted the list. Mobile does the same — this
 * screen is only ever reached by tapping a row, so the item is in cache.
 *
 * Actions mirror web exactly:
 *   - `pending` → Accept / Dismiss;
 *   - `dismissed` → Reopen (web offers it from the suggestion panel);
 *   - `accepted` / `merged` → read-only.
 * Each action goes through a native `Alert.alert` confirm — web has an
 * undo-less button plus a toast; on a phone a mis-tap is likelier, and the
 * iOS-native confirm is the platform's answer to that (apps/mobile/CLAUDE.md
 * §UI components, "Confirm / destructive prompt → Alert.alert").
 *
 * After a successful action we `router.back()`, matching web's `onResolved`
 * which clears the selection — the item is no longer in the bucket the user
 * was looking at.
 */
import { useMemo } from "react";
import { Alert, ScrollView, View } from "react-native";
import { router, useLocalSearchParams } from "expo-router";
import { useQueryClient } from "@tanstack/react-query";
import type { TriageItem, TriageItemsResponse } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { Markdown } from "@/lib/markdown";
import { ApiError } from "@/data/api";
import { triageKeys } from "@/data/queries/triage";
import {
  useAcceptTriageItem,
  useDismissTriageItem,
  useReopenTriageItem,
} from "@/data/mutations/triage";
import { useWorkspaceStore } from "@/data/workspace-store";
import { formatTriagePayload, triageStateLabel } from "@/lib/triage-display";
import { timeAgo } from "@/lib/time-ago";

export default function TriageItemDetail() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const qc = useQueryClient();

  const accept = useAcceptTriageItem();
  const dismiss = useDismissTriageItem();
  const reopen = useReopenTriageItem();
  const busy = accept.isPending || dismiss.isPending || reopen.isPending;

  // Scan every cached state bucket for this id. `getQueriesData` with the
  // feature prefix covers the four per-state infinite queries at once, so a
  // reopened item found under `dismissed` still resolves after the list
  // refetches it into `pending`.
  const item = useMemo<TriageItem | null>(() => {
    const entries = qc.getQueriesData<{ pages: TriageItemsResponse[] }>({
      queryKey: triageKeys.all(wsId),
    });
    for (const [, data] of entries) {
      for (const page of data?.pages ?? []) {
        const found = page.items.find((i) => i.id === id);
        if (found) return found;
      }
    }
    return null;
    // qc is stable; re-derive whenever the route id or workspace changes.
  }, [qc, wsId, id]);

  if (!item) {
    return (
      <View className="flex-1 items-center justify-center bg-background px-6 gap-3">
        <Text className="text-sm text-muted-foreground text-center">
          This triage item is no longer loaded. Open it again from the queue.
        </Text>
        <Button variant="outline" onPress={() => router.back()}>
          <Text>Back</Text>
        </Button>
      </View>
    );
  }

  const payloadJson = formatTriagePayload(item.payload);

  const runAccept = () =>
    Alert.alert(
      "Accept this item?",
      "An issue is created from it and the item leaves the queue.",
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Accept",
          onPress: () =>
            accept.mutate(item.id, {
              onSuccess: (res) => {
                const issue = res.issue;
                if (issue?.id && wsSlug) {
                  // Replace so Back from the issue returns to the queue, not
                  // to a detail screen whose item is gone.
                  router.replace(`/${wsSlug}/issue/${issue.id}`);
                  return;
                }
                router.back();
              },
              onError: (err) => Alert.alert(...acceptErrorAlert(err)),
            }),
        },
      ],
    );

  const runDismiss = () =>
    Alert.alert(
      "Dismiss this item?",
      "It moves to the dismissed history and can be reopened from there.",
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Dismiss",
          style: "destructive",
          onPress: () =>
            dismiss.mutate(item.id, {
              onSuccess: () => router.back(),
              onError: (err) =>
                Alert.alert("Something went wrong", errorText(err)),
            }),
        },
      ],
    );

  const runReopen = () =>
    reopen.mutate(item.id, {
      onSuccess: () => router.back(),
      onError: (err) => Alert.alert("Failed to reopen", errorText(err)),
    });

  return (
    <ScrollView
      className="flex-1 bg-background"
      contentContainerClassName="p-4 gap-5 pb-10"
    >
      <View className="gap-1">
        <Text className="text-base font-medium text-foreground">
          {item.title}
        </Text>
        <Text className="text-xs text-muted-foreground">
          {[
            item.source_name ? `From ${item.source_name}` : null,
            timeAgo(item.first_seen_at),
            item.collapse_count > 1
              ? `Seen ${item.collapse_count} times`
              : null,
            triageStateLabel(item.state),
          ]
            .filter(Boolean)
            .join(" · ")}
        </Text>
        {/* Origin link. `origin_type: "meeting"` carries the meeting id in
            `origin_id` — wired to the meetings detail screen. */}
        {item.origin_type === "meeting" && item.origin_id && wsSlug ? (
          <Button
            variant="ghost"
            size="sm"
            className="self-start px-0"
            onPress={() =>
              router.push(`/${wsSlug}/meeting/${item.origin_id}`)
            }
          >
            <Text className="text-xs">From meeting →</Text>
          </Button>
        ) : null}
        {item.resolution_reason ? (
          <Text className="text-xs text-muted-foreground">
            Reason: {item.resolution_reason}
          </Text>
        ) : null}
      </View>

      {item.state === "pending" ? (
        <View className="flex-row gap-2">
          <Button
            variant="outline"
            className="flex-1"
            disabled={busy}
            onPress={runDismiss}
          >
            <Text>{dismiss.isPending ? "Dismissing" : "Dismiss"}</Text>
          </Button>
          <Button className="flex-1" disabled={busy} onPress={runAccept}>
            <Text>{accept.isPending ? "Accepting" : "Accept"}</Text>
          </Button>
        </View>
      ) : item.state === "dismissed" ? (
        <Button variant="outline" disabled={busy} onPress={runReopen}>
          <Text>{reopen.isPending ? "Reopening" : "Reopen"}</Text>
        </Button>
      ) : null}

      {item.issue_id && wsSlug ? (
        <Button
          variant="outline"
          onPress={() => router.push(`/${wsSlug}/issue/${item.issue_id}`)}
        >
          <Text>Open issue</Text>
        </Button>
      ) : null}

      <Section title="Description">
        {item.body_markdown ? (
          <Markdown content={item.body_markdown} />
        ) : (
          <Text className="text-xs text-muted-foreground">
            No description captured
          </Text>
        )}
      </Section>

      <Section title="Captured payload">
        {payloadJson ? (
          <ScrollView horizontal showsHorizontalScrollIndicator={false}>
            <Text className="font-mono text-xs text-muted-foreground">
              {payloadJson}
            </Text>
          </ScrollView>
        ) : item.payload?.truncated ? (
          <Text className="text-xs text-muted-foreground">
            Payload too large — only its size was kept
          </Text>
        ) : (
          <Text className="text-xs text-muted-foreground">
            No payload captured
          </Text>
        )}
      </Section>
    </ScrollView>
  );
}

function Section({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <View className="gap-1.5">
      <Text className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
        {title}
      </Text>
      {children}
    </View>
  );
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : "unknown error";
}

/**
 * Mirrors web `handleAcceptError`: a 409 `duplicate` means the delivery was
 * already tracked (the item is merged, not lost), and a 402 means the
 * workspace hit its issue limit and the item stays in the queue. Anything
 * else is a generic failure.
 */
function acceptErrorAlert(err: unknown): [string, string] {
  if (err instanceof ApiError) {
    const body = (err.body ?? {}) as {
      code?: string;
      duplicate_issue_identifier?: string;
    };
    if (err.status === 409 && body.code === "duplicate") {
      const identifier = body.duplicate_issue_identifier ?? "";
      return [
        "Already tracked",
        identifier
          ? `Already tracked as ${identifier} — merged.`
          : "Already tracked — merged.",
      ];
    }
    if (err.status === 402) {
      return [
        "Issue limit reached",
        "The item stays in the queue until the workspace has room for another issue.",
      ];
    }
  }
  return ["Something went wrong", errorText(err)];
}
