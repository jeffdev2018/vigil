/**
 * "Save as note" — the sheet that turns one capture into a note.
 *
 * A formSheet route, not a centred card: it is a form with a keyboard, which
 * is exactly the row the container table in apps/mobile/CLAUDE.md Lesson 5
 * sends here. Self-contained per the same lesson — it reads the capture out
 * of the TanStack cache (the detail screen that pushed it has already
 * fetched it), calls its own mutation, and `router.back()`s twice so the
 * user lands on the inbox rather than on a capture that has left it.
 *
 * Prefill mirrors the server's own defaults so what the user sees is what
 * they would have got by accepting blindly (brain_capture.go
 * OrganizeBrainCapture): title = the suggestion's title, else the title
 * hint, else the first line of the content; tags = the suggestion's tags.
 * Content is left to the server: an empty `content` makes it render the
 * capture as markdown (the URL, the text, then the attachment as an image or
 * a link), which is strictly better than anything retyped here — but the
 * field is offered for the case where the person wants to write the note
 * properly straight away.
 */
import { useCallback, useState } from "react";
import { Alert, Pressable, ScrollView, View } from "react-native";
import { router, useLocalSearchParams } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { TextField } from "@/components/ui/text-field";
import { AutosizeTextArea } from "@/components/ui/autosize-textarea";
import { Switch } from "@/components/ui/switch";
import { MOBILE_PLACEHOLDER_COLOR } from "@/components/ui/input-tokens";
import { brainCaptureOptions } from "@/data/queries/brain";
import {
  organizeFailure,
  useOrganizeBrainCapture,
} from "@/data/mutations/brain";
import { useWorkspaceStore } from "@/data/workspace-store";
import { captureHeadline, parseTagInput } from "@/lib/brain-display";

/** Mirrors `workspaceNoteMaxTitleRunes`. */
const MAX_TITLE_CHARS = 200;

export default function OrganizeCaptureSheet() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const capture = useQuery(brainCaptureOptions(wsId, id ?? ""));
  const organize = useOrganizeBrainCapture();

  const suggestion = capture.data?.suggestion ?? null;
  const defaultTitle =
    suggestion?.title ||
    (capture.data ? captureHeadline(capture.data, MAX_TITLE_CHARS) : "");

  const [title, setTitle] = useState<string | null>(null);
  const [tagsRaw, setTagsRaw] = useState<string | null>(null);
  const [content, setContent] = useState("");
  const [pinned, setPinned] = useState(false);

  // The prefill lives in `?? default` rather than a useEffect: the capture
  // arrives asynchronously (a cold entry has no cache), and seeding state
  // from an effect would either overwrite typing or start blank.
  const titleValue = title ?? defaultTitle;
  const tagsValue = tagsRaw ?? (suggestion?.tags ?? []).join(", ");

  const submitting = organize.isPending;
  const valid = titleValue.trim() !== "" && titleValue.length <= MAX_TITLE_CHARS;

  const onSubmit = useCallback(() => {
    if (!id || !valid || submitting) return;
    organize.mutate(
      {
        id,
        input: {
          action: "note",
          title: titleValue.trim(),
          tags: parseTagInput(tagsValue),
          // Empty means "render the capture as markdown, server-side".
          content: content.trim() || undefined,
          pinned,
        },
      },
      {
        onSuccess: () => {
          // Await the server first, then leave — and leave the capture
          // detail too, since the capture is no longer raw.
          router.back();
          router.back();
        },
        onError: (err) => Alert.alert(...organizeFailure(err, "save")),
      },
    );
  }, [content, id, organize, pinned, submitting, tagsValue, titleValue, valid]);

  return (
    <ScrollView
      className="flex-1"
      contentContainerClassName="pb-8"
      stickyHeaderIndices={[0]}
      keyboardShouldPersistTaps="handled"
    >
      <View className="flex-row items-center justify-between bg-background px-4 pb-2 pt-4">
        <Text className="text-base font-semibold text-foreground">
          Save as note
        </Text>
        <Pressable
          onPress={onSubmit}
          disabled={!valid || submitting}
          hitSlop={6}
          accessibilityRole="button"
          accessibilityLabel="Save this capture as a note"
          className={`rounded-md px-3 py-1.5 ${
            !valid || submitting ? "opacity-50" : "active:bg-secondary"
          }`}
        >
          <Text className="text-sm font-semibold text-primary">
            {submitting ? "Saving…" : "Save"}
          </Text>
        </Pressable>
      </View>

      <View className="gap-4 px-4 pt-2">
        <View className="gap-1">
          <Text className="text-xs text-muted-foreground">Title</Text>
          <TextField
            value={titleValue}
            onChangeText={setTitle}
            placeholder="What is this about?"
            invalid={titleValue.length > MAX_TITLE_CHARS}
            autoFocus
          />
          {titleValue.length > MAX_TITLE_CHARS ? (
            <Text className="text-xs text-destructive">
              {`A title holds at most ${MAX_TITLE_CHARS} characters.`}
            </Text>
          ) : null}
        </View>

        <View className="gap-1">
          <Text className="text-xs text-muted-foreground">Tags</Text>
          <TextField
            value={tagsValue}
            onChangeText={setTagsRaw}
            placeholder="release, ops"
            autoCapitalize="none"
            autoCorrect={false}
          />
          <Text className="text-xs text-muted-foreground">
            Comma-separated, at most 10. They are lowercased and de-duplicated.
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
          <Text className="text-xs text-muted-foreground">
            Body (optional)
          </Text>
          <AutosizeTextArea
            value={content}
            onChangeText={setContent}
            placeholder="Leave empty to keep the capture as it is."
            placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
            minHeight={120}
            maxHeight={280}
            accessibilityLabel="Note body"
          />
        </View>
      </View>
    </ScrollView>
  );
}
