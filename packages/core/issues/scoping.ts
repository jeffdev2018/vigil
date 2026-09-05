import { useMutation } from "@tanstack/react-query";
import { api } from "../api";
import type { IssueScopingProposal } from "../types";

// Issue scoping assistant (K14): a proposal is a draft for the create form,
// never a created issue. Nothing here touches the cache.

export function useProposeIssueScoping() {
  return useMutation({
    mutationFn: (v: { raw_text: string; project_id?: string }) => api.proposeIssueScoping(v),
  });
}

/**
 * The description the form receives: the model's markdown plus, when it
 * named any, an indicative list of probable files the reviewer can edit
 * right there like the rest of the text.
 */
export function scopingDescription(p: Pick<IssueScopingProposal, "description" | "probable_files">): string {
  const files = p.probable_files.filter((f) => f.path.trim() !== "");
  if (files.length === 0) return p.description.trim();
  const lines = files.map((f) => `- \`${f.path.trim()}\`${f.reason?.trim() ? ` — ${f.reason.trim()}` : ""}`);
  return `${p.description.trim()}\n\n## Probable files (indicative)\n\n${lines.join("\n")}`;
}

/** One criterion per non-empty line, the way the form edits them. */
export function parseCriteriaLines(text: string): string[] {
  return text
    .split("\n")
    .map((l) => l.replace(/^\s*[-*\d.)]+\s*/, "").trim())
    .filter((l) => l !== "");
}
