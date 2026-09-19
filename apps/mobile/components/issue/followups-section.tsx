/**
 * "Follow-ups" (JEF-373, réveil programmé) — the pending wake-ups of this
 * issue's agent: "come back to this at 9 tomorrow with this note".
 *
 * No web reference existed when this landed (the web side is being built in
 * parallel); this mirrors the backend contract directly —
 * server/internal/handler/followups.go — the same way GoalSection mirrors
 * issue_goal.go. Sits right under the Goal section because both answer
 * "what happens next on this issue, without anyone typing".
 *
 * Hidden entirely when nothing is pending AND the workspace has no agent to
 * wake (nothing to show, nothing to schedule). Anyone who can see the issue
 * can schedule and cancel — the server gates on issue visibility, not on a
 * role, so there is no client-side permission check to mirror here.
 */
import { Alert, Pressable, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useQuery } from "@tanstack/react-query";
import { router } from "expo-router";
import type { Followup, Issue } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { issueFollowupsOptions } from "@/data/queries/followups";
import { agentListOptions } from "@/data/queries/agents";
import { useCancelFollowup } from "@/data/mutations/followups";
import { useActorLookup } from "@/data/use-actor-name";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  followupAbsoluteTime,
  followupCancelError,
  followupRelativeTime,
  followupScheduledByLabel,
} from "@/lib/followup-display";

import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

export function FollowupsSection({ issue }: { issue: Issue }) {
  const issueId = issue.id;
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { data } = useQuery(issueFollowupsOptions(wsId, issueId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const cancel = useCancelFollowup(issueId);
  const { getName } = useActorLookup();
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;

  const followups = data?.followups ?? [];
  const hasAgent = agents.some((a) => !a.archived_at);
  if (followups.length === 0 && !hasAgent) return null;

  const confirmCancel = (followup: Followup) => {
    Alert.alert(
      "Cancel this follow-up?",
      `${followup.agent_name || "The agent"} will not wake up ${followupRelativeTime(followup.fires_at, new Date())}.`,
      [
        { text: "Keep it", style: "cancel" },
        {
          text: "Cancel it",
          style: "destructive",
          onPress: () =>
            cancel.mutate(followup.id, {
              onError: (e) => Alert.alert(...followupCancelError(e)),
            }),
        },
      ],
    );
  };

  return (
    <View className="px-4 pt-4 pb-2 border-t border-border gap-2">
      <View className="flex-row items-center gap-2">
        <Ionicons name="alarm-outline" size={14} color={mutedFg} />
        <Text className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
          Follow-ups
        </Text>
        {followups.length > 0 && (
          <View className="rounded bg-muted px-1.5 py-0.5">
            <Text className="text-xs text-muted-foreground">
              {followups.length}
            </Text>
          </View>
        )}
      </View>

      {followups.map((followup) => (
        <FollowupRow
          key={followup.id}
          followup={followup}
          scheduledBy={
            followup.scheduled_by_type === "member"
              ? getName("member", followup.scheduled_by_id)
              : null
          }
          disabled={cancel.isPending}
          onCancel={() => confirmCancel(followup)}
        />
      ))}

      <Button
        variant="outline"
        size="sm"
        className="self-start"
        onPress={() =>
          wsSlug &&
          router.push({
            pathname: "/[workspace]/issue/[id]/followup",
            params: { workspace: wsSlug, id: issueId },
          })
        }
      >
        <Ionicons name="alarm-outline" size={14} color={mutedFg} />
        <Text>Schedule follow-up</Text>
      </Button>
    </View>
  );
}

function FollowupRow({
  followup,
  scheduledBy,
  disabled,
  onCancel,
}: {
  followup: Followup;
  scheduledBy: string | null;
  disabled: boolean;
  onCancel: () => void;
}) {
  const now = new Date();
  return (
    <View className="flex-row items-start gap-2 rounded-md border border-border px-3 py-2">
      <View className="flex-1 min-w-0 gap-0.5">
        <Text className="text-sm text-foreground">
          {followup.agent_name || "Agent"} · {followupRelativeTime(followup.fires_at, now)}
        </Text>
        <Text className="text-xs text-muted-foreground">
          {followupAbsoluteTime(followup.fires_at)}
        </Text>
        {!!followup.note && <Text className="text-sm">{followup.note}</Text>}
        <Text className="text-xs text-muted-foreground">
          {followupScheduledByLabel(followup, scheduledBy)}
        </Text>
      </View>
      <Pressable
        disabled={disabled}
        onPress={onCancel}
        hitSlop={8}
        accessibilityLabel="Cancel this follow-up"
        className="px-2 py-1 rounded-md active:bg-secondary"
      >
        <Text className="text-xs text-destructive">Cancel</Text>
      </Pressable>
    </View>
  );
}
