/**
 * Ranked-search snippets come from the server, which inserts `<mark>`
 * markers into the note's own text and escapes nothing else. Handing
 * that to `dangerouslySetInnerHTML` would execute whatever a note happens to
 * contain — a note is member-writable text, so that is a stored-XSS hole.
 *
 * So: escape the whole string, then bring back only the two markers we asked
 * the server for. Nothing else can survive the round trip, because after
 * escaping there is no `<` left except the ones we reintroduce.
 */

const ESCAPES: Record<string, string> = {
  "&": "&amp;",
  "<": "&lt;",
  ">": "&gt;",
  '"': "&quot;",
  "'": "&#39;",
};

function escapeHtml(text: string): string {
  return text.replace(/[&<>"']/g, (char) => ESCAPES[char] ?? char);
}

/**
 * Escaped HTML with only `<mark>` / `</mark>` live. Safe for
 * `dangerouslySetInnerHTML`; safe to render as plain text too, in which case
 * the markers simply show as text.
 */
export function renderSnippet(snippet: string): string {
  return escapeHtml(snippet)
    .replace(/&lt;mark&gt;/g, "<mark>")
    .replace(/&lt;\/mark&gt;/g, "</mark>");
}

/** The same snippet with the markers dropped, for aria labels and titles. */
export function snippetText(snippet: string): string {
  return snippet.replace(/<\/?mark>/g, "");
}
