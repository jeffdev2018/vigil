"use client";

import { useMemo } from "react";

export function HighlightText({ text, query }: { text: string; query: string }) {
  const parts = useMemo(() => {
    if (!query.trim()) return [{ text, highlight: false }];
    const terms = query.trim().split(/\s+/).filter(Boolean);
    const escaped = query.trim().replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const patterns: string[] = [escaped];
    if (terms.length > 1) {
      for (const term of terms) {
        const e = term.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
        if (e && !patterns.includes(e)) patterns.push(e);
      }
    }
    const regex = new RegExp(`(${patterns.join("|")})`, "gi");
    const result: { text: string; highlight: boolean }[] = [];
    let lastIndex = 0;
    let match: RegExpExecArray | null;
    while ((match = regex.exec(text)) !== null) {
      if (match.index > lastIndex) {
        result.push({ text: text.slice(lastIndex, match.index), highlight: false });
      }
      result.push({ text: match[0], highlight: true });
      lastIndex = regex.lastIndex;
    }
    if (lastIndex < text.length) {
      result.push({ text: text.slice(lastIndex), highlight: false });
    }
    return result.length > 0 ? result : [{ text, highlight: false }];
  }, [text, query]);

  return (
    <>
      {parts.map((part, i) =>
        part.highlight ? (
          // Reuses the same find-match tokens as the in-page find highlight
          // (packages/ui/styles/base.css ::highlight(multica-find)) so a
          // search match reads consistently whether painted via <mark> here
          // or the Custom Highlight API there.
          <mark key={i} className="bg-find-match text-find-match-foreground rounded-sm">
            {part.text}
          </mark>
        ) : (
          part.text
        ),
      )}
    </>
  );
}
