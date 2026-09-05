/**
 * Postmortem detail — mobile counterpart of web's detail aside in
 * `packages/views/postmortem/components/postmortem-page.tsx`.
 *
 * Unlike triage, this screen owns a real query: `GET /api/postmortems/{id}`
 * exists, and the screen can be entered cold from a `postmortem_ready` inbox
 * notification, so reading the list cache would leave a blank screen.
 *
 * Same sections and same order as web: origin + failure reason + age + cost,
 * quick links to the issue, then Summary / Root cause / Impact / Preventive
 * rules. Empty sections are skipped, exactly like web's `Section`.
 *
 * Approve / Discard render only while the postmortem is a draft; otherwise
 * the state is shown as a chip. Both go through a native confirm — approving
 * copies the preventive rules into the agent's memory, which is not something
 * a mis-tap should do.
 */
import { ActivityIndicator, Alert, ScrollView, View } from "react-native";
import { router, useLocalSearchParams } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { ApiError } from "@/data/api";
import { postmortemDetailOptions } from "@/data/queries/postmortem";
import {
  useApprovePostmortem,
  useDiscardPostmortem,
} from "@/data/mutations/postmortem";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  formatPostmortemCost,
  postmortemOriginLabel,
  postmortemStateLabel,
} from "@/lib/postmortem-display";
import { timeAgo } from "@/lib/time-ago";

export default function PostmortemDetail() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);

  const { data, isLoading, error, refetch } = useQuery(
    postmortemDetailOptions(wsId, id ?? ""),
  );
  const approve = useApprovePostmortem();
  const discard = useDiscardPostmortem();
  const busy = approve.isPending || discard.isPending;

  if (isLoading) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator />
      </View>
    );
  }

  // `getPostmortem` falls back to null on a shape mismatch, so a null here
  // covers both "not found" and "response we could not trust".
  if (error || !data) {
    return (
      <View className="flex-1 bg-background px-4 gap-3 pt-4">
        <Text className="text-sm text-destructive">
          Could not load this postmortem
          {error instanceof Error ? `: ${error.message}` : "."}
        </Text>
        <Button variant="outline" onPress={() => refetch()}>
          <Text>Retry</Text>
        </Button>
      </View>
    );
  }

  const item = data;
  const isDraft = item.state === "draft";
  const cost =
    typeof item.cost_usd_ticks === "number" && item.cost_usd_ticks > 0
      ? formatPostmortemCost(item.cost_usd_ticks)
      : null;

  const runApprove = () =>
    Alert.alert(
      "Approve this postmortem?",
      item.preventive_rules.length > 0 && item.agent_id
        ? "Its preventive rules are added to the agent's memory."
        : "It is kept as a resolved postmortem.",
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Approve",
          onPress: () =>
            approve.mutate(item.id, {
              onSuccess: (updated) => {
                const applied = updated?.applied_rules ?? 0;
                if (applied > 0) {
                  Alert.alert(
                    "Postmortem approved",
                    `${applied} rules added to the agent's memory.`,
                  );
                }
              },
              onError: (err) => Alert.alert(...resolveErrorAlert(err)),
            }),
        },
      ],
    );

  const runDiscard = () =>
    Alert.alert("Discard this postmortem?", "It is moved to the discarded list.", [
      { text: "Cancel", style: "cancel" },
      {
        text: "Discard",
        style: "destructive",
        onPress: () =>
          discard.mutate(item.id, {
            onError: (err) => Alert.alert(...resolveErrorAlert(err)),
          }),
      },
    ]);

  return (
    <ScrollView
      className="flex-1 bg-background"
      contentContainerClassName="p-4 gap-5 pb-10"
    >
      <View className="gap-1">
        <Text className="text-xs text-muted-foreground">
          {[
            item.failure_reason || "Failure reason",
            timeAgo(item.created_at),
            cost,
          ]
            .filter(Boolean)
            .join(" · ")}
        </Text>
        <Text className="text-xs text-muted-foreground">
          {postmortemOriginLabel(item.llm_generated)}
          {isDraft ? "" : ` · ${postmortemStateLabel(item.state)}`}
        </Text>
      </View>

      {isDraft ? (
        <View className="flex-row gap-2">
          <Button
            variant="outline"
            className="flex-1"
            disabled={busy}
            onPress={runDiscard}
          >
            <Text>{discard.isPending ? "Discarding" : "Discard"}</Text>
          </Button>
          <Button className="flex-1" disabled={busy} onPress={runApprove}>
            <Text>{approve.isPending ? "Approving" : "Approve"}</Text>
          </Button>
        </View>
      ) : null}

      {item.issue_id && wsSlug ? (
        <Button
          variant="outline"
          onPress={() => router.push(`/${wsSlug}/issue/${item.issue_id}`)}
        >
          <Text>Open issue</Text>
        </Button>
      ) : null}

      <Section title="Summary" body={item.summary} />
      <Section title="Root cause" body={item.root_cause} />
      <Section title="Impact" body={item.impact} />

      <View className="gap-1.5">
        <Text className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
          Preventive rules
        </Text>
        {item.preventive_rules.length > 0 ? (
          <>
            {item.preventive_rules.map((rule, i) => (
              <View key={i} className="flex-row gap-2">
                <Text className="text-sm text-muted-foreground">•</Text>
                <Text className="flex-1 text-sm text-foreground">{rule}</Text>
              </View>
            ))}
            {isDraft && item.agent_id ? (
              <Text className="text-xs text-muted-foreground">
                Approving adds these rules to the agent&apos;s memory.
              </Text>
            ) : null}
          </>
        ) : (
          <Text className="text-xs text-muted-foreground">
            No preventive rules suggested
          </Text>
        )}
      </View>
    </ScrollView>
  );
}

/** Skipped entirely when empty — mirrors web's `Section`. */
function Section({ title, body }: { title: string; body: string }) {
  if (!body) return null;
  return (
    <View className="gap-1.5">
      <Text className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
        {title}
      </Text>
      <Text className="text-sm text-foreground">{body}</Text>
    </View>
  );
}

/**
 * Mirrors web `handleResolveError`: a 409 means someone else already resolved
 * this postmortem, which is information rather than a failure.
 */
function resolveErrorAlert(err: unknown): [string, string] {
  if (err instanceof ApiError && err.status === 409) {
    return [
      "Already resolved",
      "This postmortem was already approved or discarded.",
    ];
  }
  return [
    "Something went wrong",
    err instanceof Error ? err.message : "unknown error",
  ];
}
