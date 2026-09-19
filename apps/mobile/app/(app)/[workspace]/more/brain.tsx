/**
 * Workspace Brain — the capture inbox and the shared notes, one screen with
 * two segments.
 *
 * "Capture first, organize later": anything worth keeping lands in the
 * inbox in one gesture, from any client; a person then turns it into a note,
 * merges it into one, or discards it. Nothing is organized behind anyone's
 * back (server/internal/handler/brain_capture.go).
 *
 * Parity targets:
 *   - Notes list = web's `NoteList`
 *     (packages/views/brain/components/brain-page.tsx): same tag chips fed
 *     by the server's `tags` facet (not derived from the filtered page),
 *     same archived toggle, same source badge, same pin glyph, same
 *     ordering (the server sorts pinned first, then updated_at).
 *   - Ranked search = `GET /api/workspace/notes/search`, debounced, with the
 *     `<mark>` snippet rendered as text (see components/brain/note-row.tsx).
 *     A non-empty field replaces the listing with hits; clearing it falls
 *     straight back, which is why `noteSearchOptions` is disabled on "".
 *   - Inbox counts = `raw_count`, which the server sends with EVERY status
 *     filter. The badge therefore reads the same number whichever filter the
 *     user is looking at, and the More-popover badge shares this cache entry.
 *
 * Where mobile diverges: web puts the list and the detail side by side in a
 * two-pane page; a phone pushes a detail screen. And the segment switch is a
 * native `SegmentedControl` — a two-way switch between two whole views is
 * what UISegmentedControl is for (rung 1 of the waterfall in
 * apps/mobile/CLAUDE.md), while the raw/organized/discarded FILTER below it
 * uses the pill row every other filtered mobile list uses (Doctrine, Triage,
 * Packs) so the two levels don't read as the same control.
 */
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ActivityIndicator, FlatList, View } from "react-native";
import SegmentedControl from "@react-native-segmented-control/segmented-control";
import { Stack, router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import type {
  BrainCapture,
  WorkspaceNote,
  WorkspaceNoteSearchHit,
} from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { IconButton } from "@/components/ui/icon-button";
import { TextField } from "@/components/ui/text-field";
import { CaptureComposer } from "@/components/brain/capture-composer";
import { CaptureRow } from "@/components/brain/capture-row";
import { NoteRow } from "@/components/brain/note-row";
import {
  brainCapturesOptions,
  brainNotesOptions,
  noteSearchOptions,
  type BrainCaptureFilter,
} from "@/data/queries/brain";
import { useWorkspaceStore } from "@/data/workspace-store";
import { captureStatusLabel } from "@/lib/brain-display";

/** Same 300ms as the workspace search modal (app/(app)/[workspace]/search.tsx). */
const SEARCH_DEBOUNCE_MS = 300;

const CAPTURE_FILTERS: BrainCaptureFilter[] = [
  "raw",
  "organized",
  "discarded",
];

const SEGMENTS = ["Inbox", "Notes"] as const;

export default function BrainScreen() {
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const [segment, setSegment] = useState(0);

  return (
    <View className="flex-1 bg-background">
      {/* The header action belongs to the whole screen, not to the Notes
          body: `Stack.Screen` options are applied through
          `navigation.setOptions` and are NOT reverted when the component
          that set them unmounts, so declaring it inside `NotesTab` would
          leave a "New note" button sitting on the Inbox segment. */}
      <Stack.Screen
        options={{
          headerRight:
            segment === 1
              ? () => (
                  <IconButton
                    name="add"
                    iconSize={24}
                    accessibilityLabel="New note"
                    onPress={() =>
                      wsSlug && router.push(`/${wsSlug}/brain/note/new`)
                    }
                  />
                )
              : undefined,
        }}
      />
      <View className="px-4 py-3">
        <SegmentedControl
          values={[...SEGMENTS]}
          selectedIndex={segment}
          onChange={(event) =>
            setSegment(event.nativeEvent.selectedSegmentIndex)
          }
        />
      </View>
      {segment === 0 ? <InboxTab /> : <NotesTab />}
    </View>
  );
}

// --- Inbox ---------------------------------------------------------------

function InboxTab() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const [filter, setFilter] = useState<BrainCaptureFilter>("raw");

  const captures = useQuery(brainCapturesOptions(wsId, filter));
  const rows = captures.data?.captures ?? [];
  const rawCount = captures.data?.raw_count ?? 0;

  const openCapture = useCallback(
    (capture: BrainCapture) => {
      if (wsSlug) router.push(`/${wsSlug}/brain/capture/${capture.id}`);
    },
    [wsSlug],
  );

  return (
    <View className="flex-1">
      <FlatList
        className="flex-1"
        data={rows}
        keyExtractor={(item) => item.id}
        contentContainerClassName="pb-4"
        keyboardDismissMode="on-drag"
        refreshing={captures.isRefetching}
        onRefresh={() => captures.refetch()}
        ListHeaderComponent={
          <View className="gap-2 px-4 pb-3">
            <View className="flex-row items-center gap-1">
              {CAPTURE_FILTERS.map((value) => {
                const active = value === filter;
                return (
                  <Button
                    key={value}
                    variant="outline"
                    size="sm"
                    onPress={() => setFilter(value)}
                    className={active ? "bg-accent" : ""}
                    accessibilityState={{ selected: active }}
                  >
                    <Text
                      className={
                        active
                          ? "text-accent-foreground"
                          : "text-muted-foreground"
                      }
                    >
                      {value === "raw"
                        ? `Inbox${rawCount > 0 ? ` ${rawCount > 99 ? "99+" : rawCount}` : ""}`
                        : captureStatusLabel(value)}
                    </Text>
                  </Button>
                );
              })}
            </View>
          </View>
        }
        ListEmptyComponent={
          captures.isLoading ? (
            <View className="py-10">
              <ActivityIndicator />
            </View>
          ) : captures.error ? (
            <ErrorState
              message="Could not load the capture inbox."
              onRetry={() => captures.refetch()}
            />
          ) : (
            <EmptyState
              title={
                filter === "raw"
                  ? "Nothing waiting"
                  : `No ${captureStatusLabel(filter).toLowerCase()} capture`
              }
              body={
                filter === "raw"
                  ? "Capture a thought, a link, a photo or a file below. Organize it into a note whenever you like."
                  : "Captures you organize or discard show up here."
              }
            />
          )
        }
        renderItem={({ item }) => (
          <CaptureRow capture={item} onPress={() => openCapture(item)} />
        )}
      />
      <CaptureComposer />
    </View>
  );
}

// --- Notes ---------------------------------------------------------------

function NotesTab() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);

  const [query, setQuery] = useState("");
  const [debounced, setDebounced] = useState("");
  const [tag, setTag] = useState("");
  const [archived, setArchived] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Debounce the field, not the query key: every distinct value would
  // otherwise be its own cache entry and its own request per keystroke.
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
    ...brainNotesOptions(wsId, { tag, archived }),
    // Skip the listing entirely while a search is running — its result is
    // about to be replaced on screen, and mobile pays for every refetch.
    enabled: !!wsId && !searching,
  });
  const search = useQuery(noteSearchOptions(wsId, debounced, { tag, archived }));

  const tags = notes.data?.tags ?? [];
  const active = searching ? search : notes;
  const rows: { note: WorkspaceNote; hit?: WorkspaceNoteSearchHit }[] =
    useMemo(() => {
      if (searching) {
        return (search.data?.notes ?? []).map((hit) => ({ note: hit, hit }));
      }
      return (notes.data?.items ?? []).map((note) => ({ note }));
    }, [notes.data, search.data, searching]);

  const openNote = useCallback(
    (note: WorkspaceNote) => {
      if (wsSlug) router.push(`/${wsSlug}/brain/note/${note.id}`);
    },
    [wsSlug],
  );

  return (
    <FlatList
        className="flex-1"
        data={rows}
        keyExtractor={(item) => item.note.id}
        contentContainerClassName="pb-10"
        keyboardDismissMode="on-drag"
        keyboardShouldPersistTaps="handled"
        refreshing={active.isRefetching}
        onRefresh={() => active.refetch()}
        ListHeaderComponent={
          <View className="gap-2 px-4 pb-3">
            <TextField
              value={query}
              onChangeText={onChangeQuery}
              placeholder="Search the Brain"
              accessibilityLabel="Search the Brain"
              autoCapitalize="none"
              autoCorrect={false}
              returnKeyType="search"
              clearButtonMode="while-editing"
            />
            {searching ? (
              <Text className="text-xs text-muted-foreground">
                {search.data?.vector === true
                  ? "Ranked by wording and meaning."
                  : "Ranked by wording."}
              </Text>
            ) : null}

            <View className="flex-row flex-wrap items-center gap-1">
              <TagChip
                label="All tags"
                active={tag === ""}
                onPress={() => setTag("")}
              />
              {tags.map((candidate) => (
                <TagChip
                  key={candidate}
                  label={candidate}
                  active={tag === candidate}
                  onPress={() => setTag(tag === candidate ? "" : candidate)}
                />
              ))}
              <TagChip
                label="Archived"
                active={archived}
                onPress={() => setArchived((value) => !value)}
              />
            </View>
          </View>
        }
        ListEmptyComponent={
          active.isLoading ? (
            <View className="py-10">
              <ActivityIndicator />
            </View>
          ) : active.error ? (
            <ErrorState
              message="Could not load the Brain."
              onRetry={() => active.refetch()}
            />
          ) : (
            <EmptyState
              title={
                searching || tag !== "" ? "Nothing matches" : "The Brain is empty"
              }
              body={
                searching || tag !== ""
                  ? "Try fewer words, or drop the tag filter. Search understands \"a phrase\", -exclusions and OR."
                  : "Notes the team writes, agents save and captures become all live here."
              }
            />
          )
        }
      renderItem={({ item }) => (
        <NoteRow
          note={item.note}
          hit={item.hit}
          onPress={() => openNote(item.note)}
        />
      )}
    />
  );
}

/**
 * Pill chip, the shape every filtered mobile list uses. The active chip stays
 * identifiable while pressed because the weight and text colour carry the
 * selection, not just the background (UI rule in the root CLAUDE.md).
 */
function TagChip({
  label,
  active,
  onPress,
}: {
  label: string;
  active: boolean;
  onPress: () => void;
}) {
  return (
    <Button
      variant="outline"
      size="sm"
      onPress={onPress}
      className={active ? "bg-accent" : ""}
      accessibilityState={{ selected: active }}
    >
      <Text
        className={
          active
            ? "font-medium text-accent-foreground"
            : "text-muted-foreground"
        }
      >
        {label}
      </Text>
    </Button>
  );
}

function EmptyState({ title, body }: { title: string; body: string }) {
  return (
    <View className="gap-1 px-6 py-10">
      <Text className="text-center text-sm font-medium text-foreground">
        {title}
      </Text>
      <Text className="text-center text-xs leading-5 text-muted-foreground">
        {body}
      </Text>
    </View>
  );
}

function ErrorState({
  message,
  onRetry,
}: {
  message: string;
  onRetry: () => void;
}) {
  return (
    <View className="gap-3 px-4 py-10">
      <Text className="text-center text-sm text-destructive">{message}</Text>
      <Button variant="outline" onPress={onRetry}>
        <Text>Retry</Text>
      </Button>
    </View>
  );
}
