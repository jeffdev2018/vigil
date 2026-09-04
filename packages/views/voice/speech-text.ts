/**
 * Turn assistant markdown into something worth reading aloud: code and
 * links are skipped or reduced to their label, list and heading markers go,
 * inline emphasis loses its asterisks. Pure, so it is unit-tested on node.
 */
export function speechTextFromMarkdown(markdown: string): string {
  let text = markdown;
  // Fenced code is unreadable aloud; say that something was skipped instead.
  text = text.replace(/```[\s\S]*?```/g, " (code) ");
  text = text.replace(/`([^`]*)`/g, "$1");
  // Images: nothing to say. Links: keep the label only.
  text = text.replace(/!\[[^\]]*\]\([^)]*\)/g, "");
  text = text.replace(/\[([^\]]+)\]\([^)]*\)/g, "$1");
  // Bare URLs are noise when spoken.
  text = text.replace(/https?:\/\/\S+/g, "");
  // Headings, quotes, list markers, tables, rules.
  text = text.replace(/^\s{0,3}#{1,6}\s+/gm, "");
  text = text.replace(/^\s*>\s?/gm, "");
  text = text.replace(/^\s*(?:[-*+]|\d+[.)])\s+/gm, "");
  text = text.replace(/^\s*\|.*\|\s*$/gm, (row) =>
    row
      .split("|")
      .map((cell) => cell.trim())
      .filter(Boolean)
      .join(", "),
  );
  text = text.replace(/^\s*(?:-{3,}|\*{3,}|_{3,})\s*$/gm, "");
  // Emphasis markers.
  text = text.replace(/(\*\*|__)(.*?)\1/g, "$2");
  text = text.replace(/(\*|_)(.*?)\1/g, "$2");
  // Collapse whitespace; keep sentence boundaries as newlines.
  text = text.replace(/[ \t]+/g, " ");
  text = text.replace(/\n{2,}/g, "\n");
  return text.trim();
}

/**
 * Split long text into utterances: browsers cut off very long utterances
 * (Chrome stops after ~15s of speech), so speak sentence groups instead.
 */
export function splitUtterances(text: string, maxChars = 240): string[] {
  const sentences = text.split(/(?<=[.!?…])\s+|\n+/).map((s) => s.trim()).filter(Boolean);
  const out: string[] = [];
  let current = "";
  for (const sentence of sentences) {
    if (current && current.length + sentence.length + 1 > maxChars) {
      out.push(current);
      current = sentence;
    } else {
      current = current ? `${current} ${sentence}` : sentence;
    }
  }
  if (current) out.push(current);
  return out;
}
