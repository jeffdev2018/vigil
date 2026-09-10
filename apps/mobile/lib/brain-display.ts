/**
 * Pure presentation helpers for the Brain screens. Every server-driven enum
 * gets a `default` branch so a kind / origin / status / source added
 * server-side degrades to a readable label instead of blanking the row
 * (apps/mobile/CLAUDE.md "State enums / transitions").
 *
 * Labels mirror web's `brain` locale namespace and
 * `packages/views/brain/components/brain-page.tsx`; mobile is English-only
 * (see lib/time-ago.ts) so they are inline here rather than in a catalogue.
 *
 * Canonical test file: lib/brain-display.test.ts. The screens keep the happy
 * path and the wiring — do not re-run this matrix through a DOM mount.
 */
import type { BrainCapture, WorkspaceNote } from "@multica/core/types";

/** Ionicons glyph per capture kind. `todo` reads as a checkbox because that
 *  is what the kind means — something to do, not something to file. */
export function captureKindIcon(
  kind: string,
): "document-text-outline" | "link-outline" | "image-outline" | "mic-outline" | "document-attach-outline" | "checkbox-outline" | "help-circle-outline" {
  switch (kind) {
    case "text":
      return "document-text-outline";
    case "link":
      return "link-outline";
    case "image":
      return "image-outline";
    case "audio":
      return "mic-outline";
    case "file":
      return "document-attach-outline";
    case "todo":
      return "checkbox-outline";
    default:
      return "help-circle-outline";
  }
}

export function captureKindLabel(kind: string): string {
  switch (kind) {
    case "text":
      return "Note";
    case "link":
      return "Link";
    case "image":
      return "Photo";
    case "audio":
      return "Voice";
    case "file":
      return "File";
    case "todo":
      return "To do";
    default:
      return kind || "Capture";
  }
}

/** Where it came in from. An unknown origin shows its own raw value — the
 *  point of the line is provenance, so hiding it would lose the signal. */
export function captureOriginLabel(origin: string): string {
  switch (origin) {
    case "web":
      return "Web";
    case "desktop":
      return "Desktop";
    case "mobile":
      return "Phone";
    case "cli":
      return "CLI";
    case "mcp":
      return "MCP";
    case "channel":
      return "Chat";
    case "agent":
      return "Agent";
    case "api":
      return "API";
    default:
      return origin || "Unknown";
  }
}

export function captureStatusLabel(status: string): string {
  switch (status) {
    case "raw":
      return "Inbox";
    case "organized":
      return "Organized";
    case "discarded":
      return "Discarded";
    default:
      return status || "Unknown";
  }
}

/**
 * The transcription chip, or null when there is nothing to say. `none` and
 * `done` are the silent states: a text capture never had audio, and a
 * finished transcript is simply the content the row already shows.
 */
export function captureTranscriptionChip(status: string): string | null {
  switch (status) {
    case "pending":
      return "Transcribing…";
    case "failed":
      return "Transcription failed";
    default:
      return null;
  }
}

/**
 * The line a capture row leads with. Same precedence the server uses when it
 * defaults a note title on organize (brain_capture.go OrganizeBrainCapture):
 * the title hint, then the first line of the content, then the URL — so what
 * the user reads in the inbox is what the note will be called.
 */
export function captureHeadline(capture: BrainCapture, max = 120): string {
  const candidates = [
    capture.title_hint,
    firstLine(capture.content),
    capture.url,
    capture.attachment?.filename ?? "",
  ];
  for (const candidate of candidates) {
    const trimmed = candidate.trim();
    if (trimmed !== "") return truncate(trimmed, max);
  }
  return "Empty capture";
}

/** The second line: the content when the headline came from somewhere else. */
export function captureSubline(capture: BrainCapture, max = 160): string | null {
  const headline = captureHeadline(capture, max);
  const body = collapseWhitespace(capture.content);
  if (body === "" || truncate(body, max) === headline) return null;
  return truncate(body, max);
}

function firstLine(value: string): string {
  return value.split("\n", 1)[0] ?? "";
}

function collapseWhitespace(value: string): string {
  return value.replace(/\s+/g, " ").trim();
}

function truncate(value: string, max: number): string {
  const chars = [...value];
  return chars.length > max ? `${chars.slice(0, max).join("")}…` : value;
}

/** Where a note came from. Mirrors web's SourceBadge, including `capture`,
 *  which the organize flow stamps on notes made out of the inbox. */
export function noteSourceLabel(source: string): string {
  switch (source) {
    case "agent":
      return "Agent";
    case "curation":
      return "Curated";
    case "capture":
      return "Captured";
    case "manual":
      return "Written";
    default:
      return source || "Written";
  }
}

/** True when the note is archived. `archived_at` is the server's own signal;
 *  an explicit null/absent check keeps a "" value from reading as archived. */
export function isNoteArchived(note: WorkspaceNote): boolean {
  return typeof note.archived_at === "string" && note.archived_at !== "";
}

/** One run of a ranked-search snippet: `hit` marks the matched words. */
export interface SnippetSegment {
  text: string;
  hit: boolean;
}

const ENTITIES: Record<string, string> = {
  amp: "&",
  lt: "<",
  gt: ">",
  quot: '"',
  "#39": "'",
  apos: "'",
  nbsp: " ",
};

function decodeEntities(value: string): string {
  return value.replace(
    /&(amp|lt|gt|quot|apos|nbsp|#39);/g,
    (_match, name: string) => ENTITIES[name] ?? _match,
  );
}

/**
 * Split a ranked-search snippet into plain-text runs, marking the ones
 * PostgreSQL `ts_headline` wrapped in `<mark>`.
 *
 * `snippet` is the note's OWN text with `<mark>` inserted and nothing
 * escaped — a note is member-writable, so the string can contain any markup
 * a person typed. Web's `renderSnippet`
 * (packages/core/brain/snippet.ts) answers that by escaping everything and
 * reintroducing the two markers, because its consumer is
 * `dangerouslySetInnerHTML`. React Native has no HTML consumer, so mobile
 * takes the equivalent-but-native route: keep the `<mark>` runs, drop every
 * other tag, decode the handful of entities `ts_headline` can emit, and hand
 * back segments the caller renders as `<Text>`. Nothing here can ever reach
 * an HTML parser.
 */
export function parseSearchSnippet(snippet: string): SnippetSegment[] {
  if (snippet === "") return [];
  const segments: SnippetSegment[] = [];
  // Consume <mark>…</mark> pairs; everything between them is a plain run.
  const marker = /<mark>([\s\S]*?)<\/mark>/g;
  let cursor = 0;
  let match: RegExpExecArray | null;
  const push = (raw: string, hit: boolean) => {
    const text = decodeEntities(stripTags(raw));
    if (text !== "") segments.push({ text, hit });
  };
  while ((match = marker.exec(snippet)) !== null) {
    if (match.index > cursor) push(snippet.slice(cursor, match.index), false);
    push(match[1] ?? "", true);
    cursor = marker.lastIndex;
  }
  if (cursor < snippet.length) push(snippet.slice(cursor), false);
  return segments;
}

/** Every remaining tag goes, including an unclosed one at the end. */
function stripTags(value: string): string {
  return value.replace(/<[^>]*>/g, "").replace(/<[^>]*$/, "");
}

/** The same snippet as plain text, for accessibility labels. */
export function searchSnippetText(snippet: string): string {
  return parseSearchSnippet(snippet)
    .map((segment) => segment.text)
    .join("");
}

/**
 * A pasted URL alone becomes a link capture rather than a text one — the
 * same inference the server makes for a body with a `url` and no `content`
 * (brain_capture.go inferBrainCaptureKind). Doing it client-side is what
 * lets the composer send `{url}` instead of `{content}` in the first place.
 *
 * Deliberately strict: one http(s) token, no surrounding words, at most the
 * 2048 characters the server accepts.
 */
export function lonelyHttpUrl(text: string): string | null {
  const trimmed = text.trim();
  if (trimmed === "" || /\s/.test(trimmed) || trimmed.length > 2048) {
    return null;
  }
  if (!/^https?:\/\/[^/?#]+/i.test(trimmed)) return null;
  return trimmed;
}

/**
 * Media clock, `m:ss` (or `h:mm:ss` past an hour) — the recorder's elapsed
 * time and the player's position/duration.
 *
 * Not `lib/format-elapsed.ts`: that one renders timing captions ("38s",
 * "1m 23s") for the chat status pill, which is the wrong shape for a
 * transport ("0:07", and "0:07 / 1:34" as a pair). Seconds in, because that
 * is what `AudioStatus` gives; the recorder divides its millis.
 */
export function formatMediaClock(seconds: number): string {
  const total = Math.max(0, Math.floor(seconds || 0));
  const s = String(total % 60).padStart(2, "0");
  const m = Math.floor(total / 60);
  if (m < 60) return `${m}:${s}`;
  return `${Math.floor(m / 60)}:${String(m % 60).padStart(2, "0")}:${s}`;
}

/** Tag input ("a, b, b") → the array the API takes. Mirrors web's
 *  `parseTags`; the server lowercases, de-duplicates and sorts. */
export function parseTagInput(raw: string): string[] {
  return raw
    .split(",")
    .map((tag) => tag.trim())
    .filter((tag) => tag !== "");
}
