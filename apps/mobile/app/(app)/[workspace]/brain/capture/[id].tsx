/**
 * One capture, in full, with everything a person needs to decide where it
 * belongs.
 *
 * Reads through `GET /api/brain/captures/{id}` rather than off the list
 * cache, so the screen survives a cold entry (deep link, a capture pushed
 * from the composer) and a post-organize refetch.
 *
 * Actions map one-to-one onto the contract:
 *   - Save as note   → organize {action:"note"}  (title / tags / pinned come
 *                      from the `organize` sheet, prefilled with the
 *                      suggestion)
 *   - Merge into…    → organize {action:"merge", note_id} (the `merge` sheet
 *                      is a searchable note picker seeded with the
 *                      suggestion's candidates)
 *   - Discard        → organize {action:"discard"}
 *   - Reopen         → reopen (only from "discarded")
 *   - Suggest        → suggest; a 503 is "no model configured", which is a
 *                      notice, not an error — the user organizes it himself
 *   - Delete         → DELETE; gone for good, the note it produced stays
 *
 * Every one of them awaits the server before navigating back or clearing
 * anything (root CLAUDE.md: flows that navigate or confirm must not be
 * optimistic). A 409 means someone else got there first, which the copy says
 * plainly rather than reporting a generic failure.
 *
 * Audio playback: the file is offered as a link, not a player. No audio
 * module is installed (`expo-av` / `expo-audio` are both absent from
 * apps/mobile/package.json), so tapping hands off to iOS, which previews
 * audio natively — the same handoff `CommentAttachmentList` uses for any
 * non-image attachment. The transcript, when the server produced one, is the
 * capture's own content and shows above it.
 */
import { useCallback } from "react";
import {
  ActivityIndicator,
  Alert,
  Linking,
  Pressable,
  ScrollView,
  View,
} from "react-native";
import { Image } from "expo-image";
import { Ionicons } from "@expo/vector-icons";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import type { BrainCapture } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { brainCaptureOptions } from "@/data/queries/brain";
import {
  organizeFailure,
  reopenFailure,
  suggestFailure,
  useDeleteBrainCapture,
  useOrganizeBrainCapture,
  useReopenBrainCapture,
  useSuggestBrainCapture,
} from "@/data/mutations/brain";
import { useWorkspaceStore } from "@/data/workspace-store";
import { resolveAttachmentUrl } from "@/lib/attachment-url";
import {
  captureKindLabel,
  captureOriginLabel,
  captureStatusLabel,
  captureTranscriptionChip,
} from "@/lib/brain-display";
import { apiErrorMessage } from "@/lib/issue-goal-display";
import { timeAgo } from "@/lib/time-ago";
import { THEME } from "@/lib/theme";
import { useColorScheme } from "@/lib/use-color-scheme";

export default function CaptureDetailScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);

  const capture = useQuery(brainCaptureOptions(wsId, id ?? ""));
  const organize = useOrganizeBrainCapture();
  const reopen = useReopenBrainCapture();
  const suggest = useSuggestBrainCapture();
  const remove = useDeleteBrainCapture();

  const busy =
    organize.isPending ||
    reopen.isPending ||
    suggest.isPending ||
    remove.isPending;

  const onDiscard = useCallback(() => {
    if (!id) return;
    Alert.alert(
      "Discard this capture?",
      "It moves out of the inbox and no note is created. You can reopen it later.",
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Discard",
          style: "destructive",
          onPress: () => {
            organize.mutate(
              { id, input: { action: "discard" } },
              {
                // Await the server, then leave — never the other way round.
                onSuccess: () => router.back(),
                onError: (err) =>
                  Alert.alert(...organizeFailure(err, "discard")),
              },
            );
          },
        },
      ],
    );
  }, [id, organize]);

  const onDelete = useCallback(() => {
    if (!id) return;
    Alert.alert(
      "Delete this capture?",
      "It is gone for good, with its file. A note it already became is untouched.",
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Delete",
          style: "destructive",
          onPress: () => {
            remove.mutate(id, {
              onSuccess: () => router.back(),
              onError: (err) =>
                Alert.alert(
                  "Could not delete the capture",
                  apiErrorMessage(err, "Try again in a moment."),
                ),
            });
          },
        },
      ],
    );
  }, [id, remove]);

  const onReopen = useCallback(() => {
    if (!id) return;
    reopen.mutate(id, {
      onError: (err) => Alert.alert(...reopenFailure(err)),
    });
  }, [id, reopen]);

  const onSuggest = useCallback(() => {
    if (!id) return;
    // A 503 is "no model configured" — a notice, not an error the user
    // caused. `suggestFailure` owns that distinction.
    suggest.mutate(id, {
      onError: (err) => Alert.alert(...suggestFailure(err)),
    });
  }, [id, suggest]);

  if (capture.isLoading) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator />
      </View>
    );
  }

  if (capture.error || !capture.data) {
    return (
      <View className="flex-1 gap-3 bg-background px-4 pt-4">
        <Text className="text-sm text-destructive">
          {apiErrorMessage(capture.error, "Could not load this capture.")}
        </Text>
        <Button variant="outline" onPress={() => capture.refetch()}>
          <Text>Retry</Text>
        </Button>
      </View>
    );
  }

  const item = capture.data;
  const isRaw = item.status === "raw";
  const isDiscarded = item.status === "discarded";
  const transcription = captureTranscriptionChip(item.transcription_status);

  return (
    <ScrollView
      className="flex-1 bg-background"
      contentContainerClassName="gap-4 px-4 py-4 pb-10"
    >
      <Stack.Screen options={{ title: captureKindLabel(item.kind) }} />

      <MetaLine capture={item} />

      {transcription ? (
        <View className="rounded-md bg-secondary px-3 py-2">
          <Text className="text-xs text-foreground">{transcription}</Text>
          {item.transcription_status === "failed" ? (
            <Text className="pt-0.5 text-xs text-muted-foreground">
              The audio is still here — save it as a note and write the gist
              yourself.
            </Text>
          ) : null}
        </View>
      ) : null}

      {item.url ? (
        <Pressable
          onPress={() => void Linking.openURL(item.url)}
          accessibilityRole="link"
          accessibilityLabel={`Open ${item.url}`}
        >
          <Text className="text-sm text-primary underline" numberOfLines={3}>
            {item.url}
          </Text>
        </Pressable>
      ) : null}

      {item.content ? (
        // Plain selectable text, not markdown: a capture is raw input, and
        // rendering it as markdown would silently reinterpret what the person
        // typed before anyone decided it was a note. The note it becomes IS
        // rendered as markdown (brain/note/[id].tsx).
        <Text selectable className="text-sm leading-6 text-foreground">
          {item.content}
        </Text>
      ) : (
        <Text className="text-sm italic text-muted-foreground">
          No text — the file below is the capture.
        </Text>
      )}

      <CaptureAttachment capture={item} />

      <SuggestionBlock capture={item} />

      <View className="gap-2 pt-2">
        {isRaw ? (
          <>
            <Button
              disabled={busy}
              onPress={() =>
                wsSlug && router.push(`/${wsSlug}/brain/capture/${item.id}/organize`)
              }
            >
              <Text>Save as note</Text>
            </Button>
            <Button
              variant="outline"
              disabled={busy}
              onPress={() =>
                wsSlug && router.push(`/${wsSlug}/brain/capture/${item.id}/merge`)
              }
            >
              <Text>Merge into…</Text>
            </Button>
            <Button variant="outline" disabled={busy} onPress={onSuggest}>
              <Text>
                {suggest.isPending
                  ? "Asking…"
                  : item.suggestion
                    ? "Suggest again"
                    : "Suggest"}
              </Text>
            </Button>
            <Button variant="outline" disabled={busy} onPress={onDiscard}>
              <Text>{organize.isPending ? "Discarding…" : "Discard"}</Text>
            </Button>
          </>
        ) : null}

        {isDiscarded ? (
          <Button variant="outline" disabled={busy} onPress={onReopen}>
            <Text>{reopen.isPending ? "Reopening…" : "Reopen"}</Text>
          </Button>
        ) : null}

        {item.status === "organized" && item.note_id ? (
          <Button
            variant="outline"
            disabled={busy}
            onPress={() =>
              wsSlug && router.push(`/${wsSlug}/brain/note/${item.note_id}`)
            }
          >
            <Text>Open the note</Text>
          </Button>
        ) : null}

        <Button variant="outline" disabled={busy} onPress={onDelete}>
          <Text className="text-destructive">
            {remove.isPending ? "Deleting…" : "Delete"}
          </Text>
        </Button>
      </View>
    </ScrollView>
  );
}

/** Kind · origin · status · age, plus who or what captured it. */
function MetaLine({ capture }: { capture: BrainCapture }) {
  const meta = [
    captureKindLabel(capture.kind),
    captureOriginLabel(capture.origin),
    captureStatusLabel(capture.status),
    capture.created_at ? timeAgo(capture.created_at) : null,
    capture.created_by_type === "agent"
      ? "by an agent"
      : capture.created_by_type === "system"
        ? "by Multica"
        : null,
  ]
    .filter(Boolean)
    .join(" · ");
  return <Text className="text-xs text-muted-foreground">{meta}</Text>;
}

/**
 * The stored file. An image gets the full width (a photographed whiteboard
 * is the capture); anything else is a tappable link that hands off to iOS,
 * which previews audio, PDFs and text natively.
 */
function CaptureAttachment({ capture }: { capture: BrainCapture }) {
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];
  const attachment = capture.attachment;
  if (!attachment) return null;

  const viewUrl = resolveAttachmentUrl(attachment.url || attachment.download_url);
  const openUrl = resolveAttachmentUrl(
    attachment.download_url || attachment.url,
  );

  if (capture.kind === "image" && viewUrl) {
    return (
      <Image
        source={{ uri: viewUrl }}
        style={{ width: "100%", aspectRatio: 4 / 3, borderRadius: 8 }}
        contentFit="contain"
        accessibilityLabel={attachment.filename || "Photo"}
      />
    );
  }

  return (
    <Pressable
      onPress={() => {
        if (openUrl) void Linking.openURL(openUrl);
      }}
      disabled={!openUrl}
      accessibilityRole="button"
      accessibilityLabel={`Open ${attachment.filename}`}
      className="flex-row items-center gap-2 rounded-md bg-secondary/60 px-3 py-2 active:opacity-80"
    >
      <Ionicons
        name={capture.kind === "audio" ? "musical-notes-outline" : "document-outline"}
        size={20}
        color={theme.mutedForeground}
      />
      <Text className="min-w-0 flex-1 text-sm text-foreground" numberOfLines={1}>
        {attachment.filename || "File"}
      </Text>
      <Ionicons name="open-outline" size={18} color={theme.mutedForeground} />
    </Pressable>
  );
}

/** What the model proposed, if a model has read this capture. */
function SuggestionBlock({ capture }: { capture: BrainCapture }) {
  const suggestion = capture.suggestion;
  if (!suggestion) return null;

  const action =
    suggestion.action === "merge"
      ? `Merge into “${suggestion.merge_note?.title ?? "an existing note"}”`
      : suggestion.action === "discard"
        ? "Discard it"
        : suggestion.action === "note"
          ? "Save it as a new note"
          : `Suggested: ${suggestion.action}`;

  return (
    <View className="gap-1 rounded-md border border-border bg-card p-3">
      <Text className="text-xs font-medium uppercase text-muted-foreground">
        Suggestion
      </Text>
      {suggestion.title ? (
        <Text className="text-sm font-medium text-foreground">
          {suggestion.title}
        </Text>
      ) : null}
      {suggestion.summary ? (
        <Text className="text-xs leading-5 text-muted-foreground">
          {suggestion.summary}
        </Text>
      ) : null}
      <Text className="text-xs text-foreground">{action}</Text>
      {suggestion.reason ? (
        <Text className="text-xs text-muted-foreground">
          {suggestion.reason}
        </Text>
      ) : null}
      {suggestion.tags.length > 0 ? (
        <View className="flex-row flex-wrap items-center gap-1 pt-0.5">
          {suggestion.tags.map((tag) => (
            <View key={tag} className="rounded bg-secondary px-1.5 py-0.5">
              <Text className="text-xs text-foreground">{tag}</Text>
            </View>
          ))}
        </View>
      ) : null}
      {suggestion.model ? (
        <Text className="pt-0.5 text-xs text-muted-foreground">
          {`Proposed by ${suggestion.model}. Nothing is filed until you say so.`}
        </Text>
      ) : null}
    </View>
  );
}

