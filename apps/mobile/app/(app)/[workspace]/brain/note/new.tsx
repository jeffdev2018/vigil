/**
 * Write a Brain note straight out, without capturing first.
 *
 * A formSheet route: a form with a keyboard (apps/mobile/CLAUDE.md Lesson 5
 * container table). Mirrors web's `NoteCreate`
 * (packages/views/brain/components/brain-page.tsx) field for field — title,
 * comma-separated tags, markdown body — plus the pin toggle web exposes from
 * the detail pane, because on a phone the note is not reachable side by side
 * afterwards.
 *
 * Awaits the server before dismissing: the create is a navigate-away flow,
 * so nothing is written into the cache first (root CLAUDE.md gate).
 */
import { useCallback, useState } from "react";
import { Alert, Pressable, ScrollView, View } from "react-native";
import { router } from "expo-router";
import { Text } from "@/components/ui/text";
import { TextField } from "@/components/ui/text-field";
import { AutosizeTextArea } from "@/components/ui/autosize-textarea";
import { Switch } from "@/components/ui/switch";
import { MOBILE_PLACEHOLDER_COLOR } from "@/components/ui/input-tokens";
import {
  noteWriteFailure,
  useCreateWorkspaceNote,
} from "@/data/mutations/brain";
import { parseTagInput } from "@/lib/brain-display";

/** Mirrors `workspaceNoteMaxTitleRunes` / `workspaceNoteMaxContentRunes`. */
const MAX_TITLE_CHARS = 200;
const MAX_CONTENT_CHARS = 20000;

export default function NewNoteSheet() {
  const create = useCreateWorkspaceNote();

  const [title, setTitle] = useState("");
  const [tagsRaw, setTagsRaw] = useState("");
  const [content, setContent] = useState("");
  const [pinned, setPinned] = useState(false);

  const submitting = create.isPending;
  const valid =
    title.trim() !== "" &&
    title.length <= MAX_TITLE_CHARS &&
    content.length <= MAX_CONTENT_CHARS;

  const onSubmit = useCallback(() => {
    if (!valid || submitting) return;
    create.mutate(
      {
        title: title.trim(),
        content,
        tags: parseTagInput(tagsRaw),
        pinned,
      },
      {
        onSuccess: () => router.back(),
        onError: (err) => Alert.alert(...noteWriteFailure(err, "create")),
      },
    );
  }, [content, create, pinned, submitting, tagsRaw, title, valid]);

  return (
    <View className="flex-1">
      <View className="flex-row items-center justify-between px-4 pb-2 pt-4">
        <Text className="text-base font-semibold text-foreground">
          New note
        </Text>
        <Pressable
          onPress={onSubmit}
          disabled={!valid || submitting}
          hitSlop={6}
          accessibilityRole="button"
          accessibilityLabel="Save this note"
          className={`rounded-md px-3 py-1.5 ${
            !valid || submitting ? "opacity-50" : "active:bg-secondary"
          }`}
        >
          <Text className="text-sm font-semibold text-primary">
            {submitting ? "Saving…" : "Save"}
          </Text>
        </Pressable>
      </View>

      <ScrollView
        className="flex-1"
        contentContainerClassName="gap-4 px-4 pb-8 pt-2"
        keyboardShouldPersistTaps="handled"
      >
        <View className="gap-1">
          <Text className="text-xs text-muted-foreground">Title</Text>
          <TextField
            value={title}
            onChangeText={setTitle}
            placeholder="What should the team remember?"
            invalid={title.length > MAX_TITLE_CHARS}
            autoFocus
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
          <Text className="text-xs text-muted-foreground">
            Comma-separated, at most 10.
          </Text>
        </View>

        <View className="flex-row items-center justify-between gap-3">
          <View className="min-w-0 flex-1">
            <Text className="text-sm text-foreground">Pin it</Text>
            <Text className="text-xs text-muted-foreground">
              Pinned notes sort first and go into every run’s context.
            </Text>
          </View>
          <Switch
            checked={pinned}
            onCheckedChange={setPinned}
            disabled={submitting}
            accessibilityLabel="Pin this note"
          />
        </View>

        <View className="gap-1">
          <Text className="text-xs text-muted-foreground">Body (markdown)</Text>
          <AutosizeTextArea
            value={content}
            onChangeText={setContent}
            placeholder="The details, the decision, the link…"
            placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
            minHeight={160}
            maxHeight={320}
            accessibilityLabel="Note body"
          />
          {content.length > MAX_CONTENT_CHARS ? (
            <Text className="text-xs text-destructive">
              {`A note holds at most ${MAX_CONTENT_CHARS} characters.`}
            </Text>
          ) : null}
        </View>
      </ScrollView>
    </View>
  );
}
