/**
 * Meeting detail — mobile parity with web
 * `packages/views/meetings/components/meeting-detail-page.tsx`.
 *
 * Read + manage. Sections in web's order: header meta, summary (markdown),
 * action items with their triage/issue links, then the transcript grouped by
 * speaker. Recording and "Finish now" are not ported — mobile does not record.
 *
 * Permissions come from one server-computed field, `can_manage` (recorder, or
 * workspace admin/owner). Its zod schema is `.catch(false)`, i.e. fail-closed:
 * an older backend that omits it hides the destructive affordances rather than
 * offering ones the server will refuse.
 *
 * Freshness: there are no meeting WS events. `meetingDetailOptions` polls
 * every 3s while the status is `summarizing`, and stops once the attempt is
 * old enough to be dead (`isMeetingSummaryStalled`) — the same bound web uses,
 * and the reason mobile does not poll a corpse on cellular.
 */
import { useMemo, useState } from "react";
import { ActivityIndicator, Alert, Pressable, ScrollView, View } from "react-native";
import { router, useLocalSearchParams } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import type { Meeting, MeetingAction } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { Markdown } from "@/lib/markdown";
import {
  isMeetingSummaryStalled,
  meetingDetailOptions,
} from "@/data/queries/meetings";
import {
  useDeleteMeeting,
  useRenameMeeting,
  useResummarizeMeeting,
} from "@/data/mutations/meetings";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  meetingActionStateLabel,
  meetingStatusLabel,
} from "@/lib/meeting-display";
import { parseTranscriptBlocks } from "@/lib/transcript-speakers";
import { timeAgo } from "@/lib/time-ago";

export default function MeetingDetail() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);

  const { data, isLoading, error, refetch } = useQuery(
    meetingDetailOptions(wsId, id ?? ""),
  );
  const rename = useRenameMeeting();
  const remove = useDeleteMeeting();
  const resummarize = useResummarizeMeeting();

  if (isLoading) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator />
      </View>
    );
  }

  // EMPTY_MEETING has an empty id — a parse fallback reads as "not loadable",
  // not as a healthy meeting.
  if (error || !data?.id) {
    return (
      <View className="flex-1 bg-background px-4 gap-3 pt-4">
        <Text className="text-sm text-destructive">
          Could not load this meeting
          {error instanceof Error ? `: ${error.message}` : "."}
        </Text>
        <Button variant="outline" onPress={() => refetch()}>
          <Text>Retry</Text>
        </Button>
      </View>
    );
  }

  const meeting = data;
  const stalled = isMeetingSummaryStalled(meeting);
  // Same gate as web: re-summarizing is pointless while recording, and while a
  // healthy finish still owns the summary.
  const canResummarize =
    meeting.can_manage &&
    meeting.status !== "recording" &&
    (meeting.status !== "summarizing" || stalled);

  const promptRename = () =>
    Alert.prompt(
      "Rename meeting",
      undefined,
      (title) => {
        const next = title.trim();
        // Empty or unchanged is a no-op — never bounce the server's 400.
        if (!next || next === meeting.title) return;
        rename.mutate(
          { id: meeting.id, title: next },
          {
            onError: (err) =>
              Alert.alert(
                "Could not rename the meeting.",
                err instanceof Error ? err.message : "unknown error",
              ),
          },
        );
      },
      "plain-text",
      meeting.title,
    );

  const confirmDelete = () =>
    Alert.alert(
      `Delete “${meeting.title}”?`,
      "The recording and its transcript are removed for good. Action items already queued in Triage are kept.",
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Delete",
          style: "destructive",
          onPress: () =>
            remove.mutate(meeting.id, {
              onSuccess: () => router.back(),
              onError: (err) =>
                Alert.alert(
                  "Could not delete the meeting.",
                  err instanceof Error ? err.message : "unknown error",
                ),
            }),
        },
      ],
    );

  const runResummarize = () =>
    resummarize.mutate(meeting.id, {
      onError: (err) =>
        Alert.alert(
          "Could not regenerate the summary.",
          err instanceof Error ? err.message : "unknown error",
        ),
    });

  return (
    <ScrollView
      className="flex-1 bg-background"
      contentContainerClassName="p-4 gap-5 pb-10"
    >
      <View className="gap-1">
        <Text className="text-base font-medium text-foreground">
          {meeting.title}
        </Text>
        <Text className="text-xs text-muted-foreground">
          {[
            meetingStatusLabel(meeting.status),
            meeting.app_name || "No app",
            `Started ${timeAgo(meeting.started_at)}`,
            meeting.ended_at ? `Ended ${timeAgo(meeting.ended_at)}` : null,
            `${meeting.segment_count} segments`,
          ]
            .filter(Boolean)
            .join(" · ")}
        </Text>
      </View>

      {meeting.can_manage ? (
        <View className="flex-row gap-2">
          <Button
            variant="outline"
            className="flex-1"
            disabled={rename.isPending}
            onPress={promptRename}
          >
            <Text>Rename</Text>
          </Button>
          <Button
            variant="outline"
            className="flex-1"
            disabled={remove.isPending}
            onPress={confirmDelete}
          >
            <Text className="text-destructive">Delete</Text>
          </Button>
        </View>
      ) : null}

      <View className="gap-1.5">
        <View className="flex-row items-center justify-between">
          <SectionTitle>Summary</SectionTitle>
          {canResummarize ? (
            <Button
              variant="ghost"
              size="sm"
              disabled={resummarize.isPending}
              onPress={runResummarize}
            >
              <Text className="text-xs">
                {resummarize.isPending ? "Regenerating…" : "Regenerate"}
              </Text>
            </Button>
          ) : null}
        </View>
        <SummaryBody meeting={meeting} stalled={stalled} />
      </View>

      {/* Hidden entirely while summarizing: the extraction is still running
          and a half-written list would read as "these are the actions". */}
      {meeting.status !== "summarizing" ? (
        <View className="gap-1.5">
          <SectionTitle>Action items</SectionTitle>
          {meeting.actions.length > 0 ? (
            meeting.actions.map((action) => (
              <ActionRow
                key={action.triage_item_id}
                action={action}
                wsSlug={wsSlug}
              />
            ))
          ) : (
            <Text className="text-xs text-muted-foreground">
              No action items were extracted
            </Text>
          )}
        </View>
      ) : null}

      <TranscriptSection transcript={meeting.transcript} />
    </ScrollView>
  );
}

function SummaryBody({
  meeting,
  stalled,
}: {
  meeting: Meeting;
  stalled: boolean;
}) {
  if (stalled) {
    return (
      <Text className="text-xs text-muted-foreground">
        Summary is taking longer than expected. The run that was writing it is
        gone — regenerate it to try again.
      </Text>
    );
  }
  if (meeting.status === "summarizing") {
    return (
      <View className="flex-row items-center gap-2">
        <ActivityIndicator size="small" />
        <Text className="text-xs text-muted-foreground">
          Writing the summary…
        </Text>
      </View>
    );
  }
  if (meeting.summary_markdown) {
    return <Markdown content={meeting.summary_markdown} />;
  }
  return (
    <Text className="text-xs text-muted-foreground">
      {meeting.summary_unavailable
        ? "No summary was written: the transcript was kept, but no language model was available."
        : "No summary yet"}
    </Text>
  );
}

/**
 * One extracted action item. Links to the created issue when the triage item
 * was accepted, otherwise to the triage item still waiting in the queue —
 * same branch as web.
 */
function ActionRow({
  action,
  wsSlug,
}: {
  action: MeetingAction;
  wsSlug: string | null;
}) {
  const target = action.issue_id
    ? { href: `/${wsSlug}/issue/${action.issue_id}`, label: "Open issue" }
    : { href: `/${wsSlug}/triage/${action.triage_item_id}`, label: "Review in Triage" };

  return (
    <View className="flex-row items-center gap-2 py-1">
      <Text className="flex-1 text-sm text-foreground" numberOfLines={2}>
        {action.title}
      </Text>
      <View className="rounded bg-secondary px-1.5 py-0.5">
        <Text className="text-xs text-muted-foreground">
          {meetingActionStateLabel(action.state)}
        </Text>
      </View>
      {wsSlug ? (
        <Pressable
          onPress={() => router.push(target.href)}
          accessibilityLabel={target.label}
          hitSlop={8}
        >
          <Text className="text-xs text-brand">Open</Text>
        </Pressable>
      ) : null}
    </View>
  );
}

/** Collapsed by default, like web — a transcript is long and rarely the point. */
function TranscriptSection({ transcript }: { transcript: string }) {
  const [open, setOpen] = useState(false);
  const blocks = useMemo(
    () => (open ? parseTranscriptBlocks(transcript) : []),
    [open, transcript],
  );

  return (
    <View className="gap-1.5">
      <View className="flex-row items-center justify-between">
        <SectionTitle>Transcript</SectionTitle>
        {transcript ? (
          <Button variant="ghost" size="sm" onPress={() => setOpen((v) => !v)}>
            <Text className="text-xs">{open ? "Hide" : "Show"}</Text>
          </Button>
        ) : null}
      </View>
      {!transcript ? (
        <Text className="text-xs text-muted-foreground">
          Nothing transcribed yet
        </Text>
      ) : open ? (
        blocks.map((block, i) => (
          <Text key={i} className="text-sm text-foreground">
            {block.speaker ? (
              <Text className="font-medium">{block.speaker}: </Text>
            ) : null}
            {block.text}
          </Text>
        ))
      ) : null}
    </View>
  );
}

function SectionTitle({ children }: { children: React.ReactNode }) {
  return (
    <Text className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
      {children}
    </Text>
  );
}
