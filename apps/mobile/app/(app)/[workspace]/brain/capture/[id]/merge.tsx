/**
 * "Merge into…" — pick the note this capture is appended to.
 *
 * A formSheet route with a search field: the long-list-plus-search row of
 * the container table in apps/mobile/CLAUDE.md Lesson 5. Self-contained —
 * it reads the capture out of cache for the suggestion's candidates, runs
 * its own search, calls its own mutation and `router.back()`s twice.
 *
 * Two sources of notes, in the order that respects the user's attention:
 *
 *   1. The suggestion's candidates, when a model has read this capture.
 *      Those are the notes the ranked search already found related, and the
 *      one the model picked is marked — so the common case is one tap with
 *      no typing.
 *   2. Everything else, through the same ranked search the Notes tab uses
 *      (`GET /api/workspace/notes/search`), debounced. The plain listing
 *      backs it up for an empty field so the sheet is never blank.
 *
 * The merge appends the capture (rendered as markdown server-side) to the
 * chosen note and unions the tags; the server refuses with a 400 if the
 * note would pass its size limit, which `organizeFailure` reports as-is.
 */
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ActivityIndicator, Alert, FlatList, Pressable, View } from "react-native";
import { router, useLocalSearchParams } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { Ionicons } from "@expo/vector-icons";
import type { WorkspaceNote } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { TextField } from "@/components/ui/text-field";
import {
  brainCaptureOptions,
  brainNotesOptions,
  noteSearchOptions,
} from "@/data/queries/brain";
import {
  organizeFailure,
  useOrganizeBrainCapture,
} from "@/data/mutations/brain";
import { useWorkspaceStore } from "@/data/workspace-store";
import { captureHeadline, noteSourceLabel } from "@/lib/brain-display";
import { timeAgo } from "@/lib/time-ago";
import { THEME } from "@/lib/theme";
import { useColorScheme } from "@/lib/use-color-scheme";

const SEARCH_DEBOUNCE_MS = 300;

/** A pickable row: either a suggestion candidate (id + title only) or a
 *  note from the search / listing. */
interface PickRow {
  id: string;
  title: string;
  /** Present for a real note; a candidate carries only id + title. */
  note?: WorkspaceNote;
  /** The one the model proposed, if any. */
  recommended: boolean;
}

export default function MergeCaptureSheet() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  const capture = useQuery(brainCaptureOptions(wsId, id ?? ""));
  const organize = useOrganizeBrainCapture();

  const [query, setQuery] = useState("");
  const [debounced, setDebounced] = useState("");
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const onChangeQuery = useCallback((value: string) => {
    setQuery(value);
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => setDebounced(value), SEARCH_DEBOUNCE_MS);
  }, []);

  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );

  const searching = debounced.trim() !== "";
  const notes = useQuery({
    ...brainNotesOptions(wsId),
    enabled: !!wsId && !searching,
  });
  const search = useQuery(noteSearchOptions(wsId, debounced, { limit: 30 }));

  const suggestion = capture.data?.suggestion ?? null;

  const rows = useMemo<PickRow[]>(() => {
    const recommendedId = suggestion?.merge_note?.id ?? "";
    const found: PickRow[] = searching
      ? (search.data?.notes ?? []).map((hit) => ({
          id: hit.id,
          title: hit.title,
          note: hit,
          recommended: hit.id === recommendedId,
        }))
      : (notes.data?.items ?? []).map((note) => ({
          id: note.id,
          title: note.title,
          note,
          recommended: note.id === recommendedId,
        }));

    if (searching) return found;

    // Candidates first, then the rest of the listing with the candidates
    // removed so nothing appears twice.
    const candidates: PickRow[] = (suggestion?.candidates ?? []).map(
      (candidate) => ({
        id: candidate.id,
        title: candidate.title,
        note: found.find((row) => row.id === candidate.id)?.note,
        recommended: candidate.id === recommendedId,
      }),
    );
    const seen = new Set(candidates.map((candidate) => candidate.id));
    return [...candidates, ...found.filter((row) => !seen.has(row.id))];
  }, [notes.data, search.data, searching, suggestion]);

  const onPick = useCallback(
    (row: PickRow) => {
      if (!id || organize.isPending) return;
      Alert.alert(
        `Merge into “${row.title}”?`,
        "The capture is appended to that note and its tags are added. The note keeps everything it already had.",
        [
          { text: "Cancel", style: "cancel" },
          {
            text: "Merge",
            onPress: () =>
              organize.mutate(
                {
                  id,
                  input: {
                    action: "merge",
                    note_id: row.id,
                    tags: suggestion?.tags,
                  },
                },
                {
                  onSuccess: () => {
                    // Server first, then leave the sheet and the capture.
                    router.back();
                    router.back();
                  },
                  onError: (err) =>
                    Alert.alert(...organizeFailure(err, "merge")),
                },
              ),
          },
        ],
      );
    },
    [id, organize, suggestion],
  );

  const active = searching ? search : notes;

  return (
    <View className="flex-1">
      <View className="gap-2 px-4 pb-2 pt-4">
        <Text className="text-base font-semibold text-foreground">
          Merge into…
        </Text>
        {capture.data ? (
          <Text className="text-xs text-muted-foreground" numberOfLines={2}>
            {captureHeadline(capture.data)}
          </Text>
        ) : null}
        <TextField
          value={query}
          onChangeText={onChangeQuery}
          placeholder="Search notes"
          accessibilityLabel="Search notes"
          autoCapitalize="none"
          autoCorrect={false}
          clearButtonMode="while-editing"
        />
      </View>

      <FlatList
        className="flex-1"
        data={rows}
        keyExtractor={(item) => item.id}
        keyboardShouldPersistTaps="handled"
        contentContainerClassName="pb-8"
        ListEmptyComponent={
          active.isLoading ? (
            <View className="py-8">
              <ActivityIndicator />
            </View>
          ) : (
            <Text className="px-6 py-8 text-center text-xs text-muted-foreground">
              {searching
                ? "No note matches. Save the capture as a new note instead."
                : "No note yet — save this capture as the first one."}
            </Text>
          )
        }
        renderItem={({ item }) => (
          <NotePickRow row={item} onPress={() => onPick(item)} />
        )}
      />
    </View>
  );
}

function NotePickRow({
  row,
  onPress,
}: {
  row: PickRow;
  onPress: () => void;
}) {
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];
  const meta = row.note
    ? [
        noteSourceLabel(row.note.source),
        ...(row.note.tags ?? []),
        row.note.updated_at ? timeAgo(row.note.updated_at) : null,
      ]
        .filter(Boolean)
        .join(" · ")
    : null;

  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={`Merge into ${row.title}`}
      className="flex-row items-center gap-2 border-b border-border px-4 py-3 active:bg-secondary/50"
    >
      <View className="min-w-0 flex-1 gap-0.5">
        <Text className="text-sm text-foreground" numberOfLines={2}>
          {row.title || "Untitled note"}
        </Text>
        {meta ? (
          <Text className="text-xs text-muted-foreground" numberOfLines={1}>
            {meta}
          </Text>
        ) : null}
      </View>
      {row.recommended ? (
        <View className="flex-row items-center gap-1 rounded bg-secondary px-1.5 py-0.5">
          <Ionicons
            name="sparkles-outline"
            size={11}
            color={theme.mutedForeground}
          />
          <Text className="text-xs text-foreground">Suggested</Text>
        </View>
      ) : null}
    </Pressable>
  );
}
