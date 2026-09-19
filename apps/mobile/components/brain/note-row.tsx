/**
 * One row of the Brain note list, and the ranked-search variant of it.
 *
 * Parity with web's `NoteList` row (packages/views/brain/components/
 * brain-page.tsx): pin glyph, title, source badge, tag chips, an archived
 * badge, then the age. Mobile stacks the metadata under the title instead of
 * putting it on one line — the same facts, a phone-width layout.
 *
 * `SnippetText` is the search half: the heading of the section that matched,
 * then the snippet (`Section · excerpt`). The server hands back the note's
 * own text with `<mark>` inserted and nothing escaped, so it is rendered through `parseSearchSnippet` into plain-text
 * runs and drawn as `<Text>`: the marked runs get weight + a highlight, and
 * every other tag in the note body is dropped. Nothing here ever reaches an
 * HTML parser — same guarantee web gets from `renderSnippet`, by the route
 * React Native allows. The highlight follows the HighlightText precedent in
 * app/(app)/[workspace]/search.tsx, hex included and for the same reason:
 * the mobile Tailwind palette carries no `yellow-*`.
 */
import { Pressable, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { WorkspaceNote, WorkspaceNoteSearchHit } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import {
  isNoteArchived,
  noteSourceLabel,
  parseSearchSnippet,
  searchSnippetText,
} from "@/lib/brain-display";
import { timeAgo } from "@/lib/time-ago";
import { THEME } from "@/lib/theme";
import { useColorScheme } from "@/lib/use-color-scheme";

const HIGHLIGHT_BG = "#fef08a";

export function NoteRow({
  note,
  hit,
  onPress,
}: {
  note: WorkspaceNote;
  /** Present when the row came from ranked search: renders its snippet. */
  hit?: WorkspaceNoteSearchHit;
  onPress: () => void;
}) {
  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];
  const archived = isNoteArchived(note);

  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={
        hit && (hit.snippet || hit.passage_heading)
          ? `${note.title}. ${[hit.passage_heading ?? "", searchSnippetText(hit.snippet)].filter((part) => part !== "").join(" · ")}`
          : note.title
      }
      className="gap-1 border-b border-border px-4 py-3 active:bg-secondary/50"
    >
      <View className="flex-row items-center gap-1.5">
        {note.pinned === true ? (
          <Ionicons name="pin" size={12} color={theme.mutedForeground} />
        ) : null}
        <Text
          className="min-w-0 flex-1 text-sm font-medium text-foreground"
          numberOfLines={2}
        >
          {note.title || "Untitled note"}
        </Text>
      </View>

      {hit && (hit.snippet || hit.passage_heading) ? (
        <SnippetText snippet={hit.snippet} heading={hit.passage_heading ?? ""} />
      ) : null}

      <View className="flex-row flex-wrap items-center gap-1">
        <Chip>{noteSourceLabel(note.source)}</Chip>
        {(note.tags ?? []).map((tag) => (
          <Chip key={tag}>{tag}</Chip>
        ))}
        {archived ? <Chip>Archived</Chip> : null}
        <Text className="text-xs text-muted-foreground">
          {note.updated_at ? timeAgo(note.updated_at) : ""}
        </Text>
      </View>
    </Pressable>
  );
}

/**
 * The section heading, then the `<mark>` runs bold and highlighted;
 * everything else plain text.
 */
export function SnippetText({ snippet, heading = "" }: { snippet: string; heading?: string }) {
  const segments = parseSearchSnippet(snippet);
  if (segments.length === 0 && heading === "") return null;
  return (
    <Text className="text-xs text-muted-foreground" numberOfLines={3}>
      {heading !== "" ? (
        <Text className="font-medium text-foreground">
          {heading}
          {segments.length > 0 ? " · " : ""}
        </Text>
      ) : null}
      {segments.map((segment, index) =>
        segment.hit ? (
          <Text
            key={index}
            className="font-semibold text-foreground"
            style={{ backgroundColor: HIGHLIGHT_BG }}
          >
            {segment.text}
          </Text>
        ) : (
          <Text key={index}>{segment.text}</Text>
        ),
      )}
    </Text>
  );
}

function Chip({ children }: { children: React.ReactNode }) {
  return (
    <View className="rounded bg-secondary px-1.5 py-0.5">
      <Text className="text-xs text-foreground">{children}</Text>
    </View>
  );
}
