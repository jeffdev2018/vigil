/**
 * Meetings list — mobile parity with web
 * `packages/views/meetings/components/meetings-page.tsx`.
 *
 * Same offset pagination (the endpoint has no cursor), same row fields
 * (status dot, title, app name, segment count, action count, age), and the
 * same permission signal: the row menu only exists when `can_manage` is true,
 * which the server computes — mobile never loads the member list to work out
 * a role.
 *
 * Deliberately NOT ported: recording. Web's "Record a meeting" button, the
 * capability banner and the MediaRecorder flow are browser APIs; mobile reads
 * meetings recorded elsewhere. The client-side title search is also skipped
 * for now — one text field on a phone list that already paginates is not
 * worth the keyboard.
 */
import { useMemo } from "react";
import { ActivityIndicator, Alert, FlatList, Pressable, View } from "react-native";
import { useInfiniteQuery } from "@tanstack/react-query";
import { router } from "expo-router";
import type { Meeting } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import {
  flattenMeetingPages,
  meetingListOptions,
} from "@/data/queries/meetings";
import { useDeleteMeeting } from "@/data/mutations/meetings";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  meetingStatusDotClass,
  meetingStatusLabel,
} from "@/lib/meeting-display";
import { timeAgo } from "@/lib/time-ago";
import { cn } from "@/lib/utils";

export default function MeetingsPage() {
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const deleteMeeting = useDeleteMeeting();

  const {
    data,
    isLoading,
    error,
    refetch,
    isRefetching,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useInfiniteQuery(meetingListOptions(wsId));

  const meetings = useMemo(
    () => flattenMeetingPages(data?.pages),
    [data?.pages],
  );

  const confirmDelete = (meeting: Meeting) =>
    Alert.alert(
      `Delete “${meeting.title}”?`,
      "The recording and its transcript are removed for good. Action items already queued in Triage are kept.",
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Delete",
          style: "destructive",
          onPress: () =>
            deleteMeeting.mutate(meeting.id, {
              onError: (err) =>
                Alert.alert(
                  "Could not delete the meeting.",
                  err instanceof Error ? err.message : "unknown error",
                ),
            }),
        },
      ],
    );

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
          Could not load meetings:{" "}
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
      data={meetings}
      keyExtractor={(item) => item.id}
      ItemSeparatorComponent={() => <View className="h-px bg-border ml-4" />}
      contentContainerClassName="pb-6"
      ListEmptyComponent={
        <Text className="px-6 py-12 text-center text-sm text-muted-foreground">
          No meetings yet. Record a conversation from the web or desktop app
          and Multica transcribes it, writes a summary, and queues every action
          item in Triage.
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
        <MeetingRow
          meeting={item}
          onPress={() => {
            if (wsSlug) router.push(`/${wsSlug}/meeting/${item.id}`);
          }}
          // Long-press for the destructive action: web puts it behind a
          // hover-revealed row menu, which has no phone equivalent.
          onLongPress={item.can_manage ? () => confirmDelete(item) : undefined}
        />
      )}
    />
  );
}

function MeetingRow({
  meeting,
  onPress,
  onLongPress,
}: {
  meeting: Meeting;
  onPress: () => void;
  onLongPress?: () => void;
}) {
  const meta = [
    meeting.app_name || "No app",
    `${meeting.segment_count} segments`,
    meeting.action_count > 0 ? `${meeting.action_count} actions` : null,
    timeAgo(meeting.started_at),
  ].filter(Boolean);

  return (
    <Pressable
      onPress={onPress}
      onLongPress={onLongPress}
      className="flex-row items-center gap-3 px-4 py-3 active:bg-secondary"
      accessibilityLabel={meeting.title}
    >
      <View
        accessibilityLabel={meetingStatusLabel(meeting.status)}
        className={cn(
          "size-2 rounded-full",
          meetingStatusDotClass(meeting.status),
        )}
      />
      <View className="flex-1 min-w-0 gap-0.5">
        <Text className="text-sm text-foreground" numberOfLines={1}>
          {meeting.title}
        </Text>
        <Text className="text-xs text-muted-foreground" numberOfLines={1}>
          {meta.join(" · ")}
        </Text>
      </View>
    </Pressable>
  );
}
