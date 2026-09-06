"use client";

import { useState } from "react";
import type { RunPlan as RunPlanData, RunPlanItem } from "@multica/core/types";
import { UnicodeSpinner } from "@multica/ui/components/common/unicode-spinner";
import { useT } from "../../i18n";

// The living run plan (F04): the checklist a run publishes and replaces as it
// works. Shared by the execution log, the chat footer and the transcript, so it
// lives here rather than in any one of them.
//
// It renders what the server sent and nothing else. `status` is an open string
// on the wire — the write side is closed, so only a NEWER server can produce a
// value this build does not know — and an unknown one gets a neutral bullet
// rather than being dropped or guessed into one of the three known states.

/** How many items fit before the list starts hiding the parts nobody is at. */
const WINDOW_THRESHOLD = 5;

/** Done / total, for the counter the run row shows next to its status. */
export function runPlanProgress(plan: RunPlanData): { done: number; total: number } {
  return {
    done: plan.items.filter((item) => item.status === "done").length,
    total: plan.items.length,
  };
}

/**
 * Index the reader cares about: the item in progress, else the first one not
 * yet done (what happens next), else the last (everything is finished).
 */
function anchorIndex(items: RunPlanItem[]): number {
  const active = items.findIndex((item) => item.status === "in_progress");
  if (active !== -1) return active;
  const next = items.findIndex((item) => item.status !== "done");
  return next !== -1 ? next : items.length - 1;
}

interface RunPlanProps {
  plan: RunPlanData;
  /** A finished run's last plan is history: shown, but not as live progress. */
  muted?: boolean;
  className?: string;
}

export function RunPlan({ plan, muted = false, className }: RunPlanProps) {
  const { t } = useT("common");
  const [expanded, setExpanded] = useState(false);
  const items = plan.items;

  // No plan is no block at all — an empty checklist would assert the run has
  // nothing to do, which is a different (and wrong) statement.
  if (items.length === 0) return null;

  const windowed = items.length > WINDOW_THRESHOLD && !expanded;
  // The active item plus its two neighbours: enough to read where the run is
  // and where it is going without paying for a plan nobody scrolls.
  const anchor = anchorIndex(items);
  const from = windowed ? Math.max(0, Math.min(anchor - 1, items.length - 3)) : 0;
  const to = windowed ? from + 3 : items.length;
  const { done, total } = runPlanProgress(plan);

  return (
    <div className={`space-y-0.5 ${muted ? "opacity-60" : ""} ${className ?? ""}`}>
      <ul
        // Explicit role: `list-none` styling drops the implicit list role in
        // Safari/VoiceOver, and the count is exactly what a screen reader needs
        // here — how many steps there are, and how far through them the run is.
        role="list"
        aria-label={t(($) => $.run_plan.progress_aria, { done, total })}
        className="space-y-0.5"
      >
        {items.slice(from, to).map((item, i) => (
          <PlanItemRow key={from + i} item={item} />
        ))}
      </ul>
      {items.length > WINDOW_THRESHOLD && (
        <button
          type="button"
          onClick={() => setExpanded(!expanded)}
          className="rounded px-1 py-0.5 text-micro text-muted-foreground transition-colors hover:bg-accent/40 hover:text-foreground"
        >
          {expanded
            ? t(($) => $.run_plan.show_less)
            : t(($) => $.run_plan.show_all, { count: items.length })}
        </button>
      )}
    </div>
  );
}

function PlanItemRow({ item }: { item: RunPlanItem }) {
  const { t } = useT("common");
  const status = item.status;
  return (
    <li className="flex items-start gap-1.5 text-caption">
      <span className="mt-px w-[1ch] shrink-0 text-center font-mono text-muted-foreground">
        {status === "in_progress" ? (
          <UnicodeSpinner />
        ) : (
          // Filled = done, hollow = still to do, mid dot = a status this build
          // does not know. All three are one character wide so the text column
          // starts at the same place whatever the run reports.
          <span aria-hidden="true">
            {status === "done" ? "●" : status === "pending" ? "○" : "·"}
          </span>
        )}
      </span>
      <span className="sr-only">{statusLabel(status, t)}</span>
      <span
        // The title is the escape hatch for a step whose text is longer than
        // the 288px sidebar; truncate never silently loses it.
        title={item.text}
        className={`min-w-0 flex-1 truncate ${
          status === "done"
            ? "text-muted-foreground line-through decoration-muted-foreground/40"
            : status === "in_progress"
              ? "text-foreground"
              : "text-muted-foreground"
        }`}
      >
        {item.text}
      </span>
    </li>
  );
}

type CommonT = ReturnType<typeof useT<"common">>["t"];

function statusLabel(status: string, t: CommonT): string {
  switch (status) {
    case "done":
      return t(($) => $.run_plan.status_done);
    case "in_progress":
      return t(($) => $.run_plan.status_in_progress);
    case "pending":
      return t(($) => $.run_plan.status_pending);
    default:
      // A status only a newer server can send. Naming it beats translating it
      // into one of the three this build knows, which would be a guess.
      return status;
  }
}
