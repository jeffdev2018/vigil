/**
 * One Brain note: read it, edit it, pin it, archive it, delete it.
 *
 * Reads through `GET /api/workspace/notes/{id}` rather than the list cache
 * so it survives a cold entry (a capture's "Open the note", a deep link)
 * and so the post-409 reload has a single source of truth.
 *
 * Parity with web's `NoteDetailBody`
 * (packages/views/brain/components/brain-page.tsx):
 *   - the body renders as markdown through the same renderer mobile uses for
 *     issue descriptions and comments (lib/markdown), because that is what
 *     web's `RichContent` does for it;
 *   - the same actions, in the same shape: edit (title / tags / content),
 *     pin, archive, and the same 409 sentence — "someone else wrote first,
 *     reload and re-apply";
 *   - the draft is re-seeded from the note whenever the reader is NOT
 *     editing, so a realtime update or the curation pass rewriting the note
 *     stays visible without ever discarding typing.
 *
 * What mobile adds: delete. Web's Brain page has no delete button, but the
 * endpoint exists and is narrower than read/write — only a workspace
 * owner/admin or the note's own author may remove one — so a 403 is a real
 * outcome and its sentence is surfaced verbatim (`noteWriteFailure`).
 *
 * Every write awaits the server. `revision` is sent on every PATCH: it is
 * the token the server issued, never a locally-guessed one, which is why
 * none of this is optimistic.
 */
import { useCallback, useEffect, useState } from "react";
import { ActivityIndicator, Alert, ScrollView, View } from "react-native";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { IconButton } from "@/components/ui/icon-button";
import { TextField } from "@/components/ui/text-field";
import { AutosizeTextArea } from "@/components/ui/autosize-textarea";
import { MOBILE_PLACEHOLDER_COLOR } from "@/components/ui/input-tokens";
import { brainNoteOptions } from "@/data/queries/brain";
import {
  noteWriteFailure,
  useDeleteWorkspaceNote,
  useSetWorkspaceNoteArchived,
  useUpdateWorkspaceNote,
} from "@/data/mutations/brain";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  isNoteArchived,
  noteSourceLabel,
  parseTagInput,
} from "@/lib/brain-display";
import { apiErrorMessage } from "@/lib/issue-goal-display";
import { Markdown } from "@/lib/markdown";
import { timeAgo } from "@/lib/time-ago";

/** Mirrors `workspaceNoteMaxTitleRunes` / `workspaceNoteMaxContentRunes`. */
const MAX_TITLE_CHARS = 200;
const MAX_CONTENT_CHARS = 20000;

export default function NoteDetailScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const note = useQuery(brainNoteOptions(wsId, id ?? ""));

  const update = useUpdateWorkspaceNote();
  const setArchived = useSetWorkspaceNoteArchived();
  const remove = useDeleteWorkspaceNote();

  const [editing, setEditing] = useState(false);
  // The revision the draft was opened on. `data.revision` keeps moving
  // while the form is open (realtime updates refetch the note), and sending
  // the moved value made the server accept a save built on stale fields —
  // a concurrent edit was silently overwritten instead of answering 409.
  const [editRevision, setEditRevision] = useState(0);
  const [title, setTitle] = useState("");
  const [tagsRaw, setTagsRaw] = useState("");
  const [content, setContent] = useState("");

  const data = note.data;

  // Re-seed the draft while NOT editing: a realtime update, an agent run or
  // the curation pass can rewrite the note under an open reader, and the
  // screen must stay current without ever throwing away typing.
  useEffect(() => {
    if (editing || !data) return;
    setTitle(data.title);
    setTagsRaw((data.tags ?? []).join(", "));
    setContent(data.content);
  }, [editing, data]);

  const busy = update.isPending || setArchived.isPending || remove.isPending;

  const onSave = useCallback(() => {
    if (!data || busy) return;
    if (title.trim() === "" || title.length > MAX_TITLE_CHARS) {
      Alert.alert(
        "A title is required",
        `Give the note a title of at most ${MAX_TITLE_CHARS} characters.`,
      );
      return;
    }
    if (content.length > MAX_CONTENT_CHARS) {
      Alert.alert(
        "Too long",
        `A note holds at most ${MAX_CONTENT_CHARS} characters.`,
      );
      return;
    }
    update.mutate(
      {
        id: data.id,
        input: {
          title: title.trim(),
          content,
          tags: parseTagInput(tagsRaw),
          // The revision the draft was READ on. The server refuses a 0 and
          // answers 409 when the note moved since — never guessed locally.
          revision: editRevision,
        },
      },
      {
        onSuccess: () => setEditing(false),
        onError: (err) => {
          Alert.alert(...noteWriteFailure(err, "save"));
          // A conflict means the local draft is built on a version that no
          // longer exists; refetch so the reader shows what the server has
          // and the user can re-apply on top of it.
          void note.refetch();
        },
      },
    );
  }, [busy, content, data, editRevision, note, tagsRaw, title, update]);

  const onTogglePin = useCallback(() => {
    if (!data || busy) return;
    update.mutate(
      {
        id: data.id,
        input: { pinned: data.pinned !== true, revision: data.revision },
      },
      { onError: (err) => Alert.alert(...noteWriteFailure(err, "pin")) },
    );
  }, [busy, data, update]);

  const onToggleArchive = useCallback(() => {
    if (!data || busy) return;
    const archived = isNoteArchived(data);
    setArchived.mutate(
      { id: data.id, archived: !archived },
      {
        onError: (err) =>
          Alert.alert(...noteWriteFailure(err, archived ? "unarchive" : "archive")),
      },
    );
  }, [busy, data, setArchived]);

  const onDelete = useCallback(() => {
    if (!data || busy) return;
    Alert.alert(
      `Delete “${data.title}”?`,
      "The note is gone for good, for everyone in the workspace. Archive it instead if you only want it out of the way.",
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Delete",
          style: "destructive",
          onPress: () =>
            remove.mutate(data.id, {
              // Await the server before leaving — a 403 must keep the user
              // on the note, not drop them on a list that still has it.
              onSuccess: () => router.back(),
              onError: (err) => Alert.alert(...noteWriteFailure(err, "delete")),
            }),
        },
      ],
    );
  }, [busy, data, remove]);

  if (note.isLoading) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator />
      </View>
    );
  }

  if (note.error || !data) {
    return (
      <View className="flex-1 gap-3 bg-background px-4 pt-4">
        <Text className="text-sm text-destructive">
          {apiErrorMessage(note.error, "Could not load this note.")}
        </Text>
        <Button variant="outline" onPress={() => note.refetch()}>
          <Text>Retry</Text>
        </Button>
      </View>
    );
  }

  const archived = isNoteArchived(data);
  const meta = [
    noteSourceLabel(data.source),
    archived ? "Archived" : null,
    data.pinned === true ? "Pinned" : null,
    data.updated_at ? `updated ${timeAgo(data.updated_at)}` : null,
    `revision ${data.revision}`,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <ScrollView
      className="flex-1 bg-background"
      contentContainerClassName="gap-4 px-4 py-4 pb-10"
      keyboardShouldPersistTaps="handled"
    >
      <Stack.Screen
        options={{
          title: "Note",
          headerRight: () =>
            editing ? undefined : (
              <>
                <IconButton
                  name={data.pinned === true ? "pin" : "pin-outline"}
                  onPress={onTogglePin}
                  disabled={busy}
                  accessibilityLabel={
                    data.pinned === true ? "Unpin this note" : "Pin this note"
                  }
                />
                <IconButton
                  name="create-outline"
                  onPress={() => {
                    setEditRevision(data.revision);
                    setEditing(true);
                  }}
                  disabled={busy}
                  accessibilityLabel="Edit this note"
                />
              </>
            ),
        }}
      />

      <Text className="text-xs text-muted-foreground">{meta}</Text>
      {data.merged_into ? (
        <Text className="text-xs text-muted-foreground">
          The curation pass folded this note into another one.
        </Text>
      ) : null}

      {editing ? (
        <>
          <View className="gap-1">
            <Text className="text-xs text-muted-foreground">Title</Text>
            <TextField
              value={title}
              onChangeText={setTitle}
              placeholder="What is this about?"
              invalid={title.length > MAX_TITLE_CHARS}
            />
          </View>
          <View className="gap-1">
            <Text className="text-xs text-muted-foreground">Tags</Text>
            <TextField
              value={tagsRaw}
              onChangeText={setTagsRaw}
              placeholder="release, ops"
              autoCapitalize="none"
              autoCorrect={false}
            />
          </View>
          <View className="gap-1">
            <Text className="text-xs text-muted-foreground">
              Body (markdown)
            </Text>
            <AutosizeTextArea
              value={content}
              onChangeText={setContent}
              placeholder="What should the team remember?"
              placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
              minHeight={200}
              maxHeight={420}
              accessibilityLabel="Note body"
            />
          </View>
          <View className="flex-row gap-2">
            <Button
              className="flex-1"
              variant="outline"
              disabled={busy}
              onPress={() => setEditing(false)}
            >
              <Text>Cancel</Text>
            </Button>
            <Button className="flex-1" disabled={busy} onPress={onSave}>
              <Text>{update.isPending ? "Saving…" : "Save"}</Text>
            </Button>
          </View>
        </>
      ) : (
        <>
          <Text className="text-lg font-semibold text-foreground">
            {data.title || "Untitled note"}
          </Text>
          {(data.tags ?? []).length > 0 ? (
            <View className="flex-row flex-wrap items-center gap-1">
              {(data.tags ?? []).map((tag) => (
                <View key={tag} className="rounded bg-secondary px-1.5 py-0.5">
                  <Text className="text-xs text-foreground">{tag}</Text>
                </View>
              ))}
            </View>
          ) : null}
          {data.content ? (
            <Markdown content={data.content} />
          ) : (
            <Text className="text-sm italic text-muted-foreground">
              This note has no body yet.
            </Text>
          )}

          <View className="gap-2 pt-2">
            <Button variant="outline" disabled={busy} onPress={onToggleArchive}>
              <Text>
                {setArchived.isPending
                  ? "Saving…"
                  : archived
                    ? "Unarchive"
                    : "Archive"}
              </Text>
            </Button>
            <Button variant="outline" disabled={busy} onPress={onDelete}>
              <Text className="text-destructive">
                {remove.isPending ? "Deleting…" : "Delete"}
              </Text>
            </Button>
          </View>
        </>
      )}
    </ScrollView>
  );
}
