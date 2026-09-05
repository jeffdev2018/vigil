/**
 * Speaker-aware view of a meeting transcript.
 *
 * The batch path can label speakers: when the provider diarizes, the server
 * writes one `"<label>: <text>"` line per segment (see formatText in
 * server/pkg/stt/client.go — the label is "Speaker N" or whatever name the
 * provider returned). The live path has no speakers at all, so its lines are
 * plain text and stay that way.
 *
 * Parsing is deliberately timid: a colon inside a sentence ("On a décidé: on
 * livre vendredi") must not be read as a speaker change, so a label has to be
 * short, single-line, and free of sentence punctuation.
 */

/** One rendered block: a run of consecutive lines from the same speaker. */
export interface TranscriptBlock {
  /** null when the line carried no label — the live path, or an undiarized run. */
  speaker: string | null;
  text: string;
}

/**
 * One stored line, with the index it occupies in the transcript. That index is
 * the `seq` the segment-edit endpoint addresses (PATCH
 * /api/meetings/{id}/segments/{seq}), so it counts blank lines too even though
 * nothing renders them.
 */
export interface TranscriptLine extends TranscriptBlock {
  index: number;
}

/** Longest label we will believe. Real ones are "Speaker 3" or a first name. */
const MAX_SPEAKER_LABEL = 40;

function splitLabel(line: string): { speaker: string; text: string } | null {
  const at = line.indexOf(":");
  if (at <= 0 || at > MAX_SPEAKER_LABEL) return null;
  // The server writes `label + ": " + text`. Requiring the space is what keeps
  // "https://example.test" from reading as a speaker named "https".
  if (line[at + 1] !== " ") return null;
  const speaker = line.slice(0, at).trim();
  // A label is a name, not a sentence. Anything with sentence punctuation, a
  // second colon's worth of prose, or nothing after the colon is just text.
  if (!speaker || /[.!?,;]/.test(speaker)) return null;
  const text = line.slice(at + 1).trim();
  if (!text) return null;
  return { speaker, text };
}

/**
 * Every non-empty stored line, split into its speaker label and its text, with
 * the line index kept. This is what the transcript editor addresses: editing
 * merges nothing, because a saved edit rewrites exactly one line.
 */
export function parseTranscriptLines(transcript: string): TranscriptLine[] {
  const lines: TranscriptLine[] = [];
  transcript.split("\n").forEach((raw, index) => {
    const line = raw.trim();
    if (!line) return;
    const labelled = splitLabel(line);
    lines.push({
      index,
      speaker: labelled?.speaker ?? null,
      text: labelled?.text ?? line,
    });
  });
  return lines;
}

/**
 * Rebuilds the stored line from an edited paragraph, so a diarized transcript
 * keeps its "Speaker 1: " prefix. Exact inverse of splitLabel — the server
 * stores whatever this returns and never re-parses labels itself.
 */
export function formatTranscriptLine(speaker: string | null, text: string): string {
  const body = text.trim();
  return speaker ? `${speaker}: ${body}` : body;
}

/**
 * Turns a stored transcript into blocks to render. Consecutive lines from the
 * same speaker are joined so one person talking for a minute reads as one
 * paragraph rather than a stack of fragments.
 */
export function parseTranscriptBlocks(transcript: string): TranscriptBlock[] {
  const blocks: TranscriptBlock[] = [];
  for (const { speaker, text } of parseTranscriptLines(transcript)) {
    const last = blocks[blocks.length - 1];
    if (last && last.speaker === speaker) {
      last.text = `${last.text} ${text}`;
      continue;
    }
    blocks.push({ speaker, text });
  }
  return blocks;
}

/** True when at least one block is attributed — i.e. worth showing labels. */
export function hasSpeakers(blocks: TranscriptBlock[]): boolean {
  return blocks.some((block) => block.speaker !== null);
}
