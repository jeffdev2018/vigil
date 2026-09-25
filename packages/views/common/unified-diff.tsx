"use client";

import { diffUnifiedLines } from "@multica/core/issues/run-group";
import { cn } from "@multica/ui/lib/utils";

/**
 * A recorded unified patch, rendered line by line. Shared by the race
 * attempt columns (run-group-section) and the execution log's per-run
 * worktree block (JEF-255).
 *
 * The two no-patch states are sentences, not an empty box: `truncated` means
 * the run did change things and the patch was too large to store — saying
 * "no changes" there would be a lie. The labels stay caller-owned props so
 * each surface picks them from its own i18n namespace.
 */
export function UnifiedDiff({
  diff,
  truncated,
  emptyLabel,
  truncatedLabel,
  testid,
}: {
  diff: string | null;
  truncated: boolean;
  /** Shown when the run recorded no patch at all. */
  emptyLabel: string;
  /** Shown when the patch exists but was too large to store. */
  truncatedLabel: string;
  testid?: string;
}) {
  if (truncated) {
    return (
      <p data-testid={testid ? `${testid}-truncated` : undefined} className="text-muted-foreground">
        {truncatedLabel}
      </p>
    );
  }
  if (!diff) {
    return <p className="text-muted-foreground">{emptyLabel}</p>;
  }
  return (
    <pre data-testid={testid} className="max-h-64 overflow-auto rounded bg-muted/40 p-2 font-mono text-caption">
      {diffUnifiedLines(diff).map((line, i) => (
        <div
          key={i}
          className={cn(
            "whitespace-pre",
            line.kind === "added" && "bg-success/15",
            line.kind === "removed" && "bg-destructive/15",
            (line.kind === "hunk" || line.kind === "meta") && "text-muted-foreground",
          )}
        >
          {line.text || " "}
        </div>
      ))}
    </pre>
  );
}
