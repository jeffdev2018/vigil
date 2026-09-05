/**
 * Pure display helpers for the issue "Agent changes" section (K69 undo).
 *
 * Mirrors `groupEffectsByRun` / `effectState` / `effectField` in
 * packages/core/issues/agent-effects.ts and `describeEffect` /
 * `reasonKey` in packages/views/issues/components/agent-effects-section.tsx
 * (copied, not imported: that core module also exports TanStack hooks bound
 * to web's api client). Strings mirror `agent_effects.*` in
 * packages/views/locales/en/issues.json. Mobile is English-only today; when
 * mobile ships i18n, mirror that namespace.
 */
import type { AgentEffect, UndoReport } from "@/data/schemas";

export type AgentEffectState =
  | "pending"
  | "reversed"
  | "expired"
  | "not_reversible"
  | "failed"
  | "held"
  | "approved"
  | "rejected";

export interface AgentEffectRun {
  task_id: string;
  agent_name: string;
  effects: AgentEffect[];
  /** effects that an undo of the run would reverse now */
  pending: number;
}

/** pending = can be reversed now; held / approved / rejected for a preview-mode write. */
export function effectState(e: AgentEffect): AgentEffectState {
  if (e.status === "pending") return "held";
  if (e.status === "approved") return "approved";
  if (e.status === "rejected") return "rejected";
  if (e.reversed_at) return "reversed";
  if (!e.reversible) return "not_reversible";
  if (e.reverse_error) return "failed";
  if (!e.within_window) return "expired";
  return "pending";
}

/** Group a newest-first effect list by run, keeping the list order. */
export function groupEffectsByRun(effects: AgentEffect[]): AgentEffectRun[] {
  const runs: AgentEffectRun[] = [];
  const byTask = new Map<string, AgentEffectRun>();
  for (const e of effects) {
    let run = byTask.get(e.task_id);
    if (!run) {
      run = {
        task_id: e.task_id,
        agent_name: e.agent_name,
        effects: [],
        pending: 0,
      };
      byTask.set(e.task_id, run);
      runs.push(run);
    }
    run.effects.push(e);
    if (effectState(e) === "pending") run.pending += 1;
  }
  return runs;
}

/** Mirrors issues.json `agent_effects.states.*`; pending has no badge. */
export const EFFECT_STATE_LABEL: Record<
  Exclude<AgentEffectState, "pending">,
  string
> = {
  reversed: "reversed",
  expired: "window expired",
  not_reversible: "not reversible",
  failed: "reversal failed",
  held: "awaiting approval",
  approved: "approved",
  rejected: "discarded",
};

const str = (v: unknown) => (typeof v === "string" ? v : "");

/** Mirrors web `describeEffect`; unknown kinds fall back to the raw kind. */
export function describeEffect(e: AgentEffect): string {
  const from = String(e.before["value"] ?? "");
  const to = String(e.after["value"] ?? "");
  switch (e.kind) {
    case "issue_status":
      return `Status ${from} → ${to}`;
    case "issue_field": {
      const field = str(e.before["field"] ?? e.after["field"]);
      if (field === "assignee") return "Assignee changed";
      return `${field}: ${from || "∅"} → ${to || "∅"}`;
    }
    case "comment_create":
      return `Comment: ${String(e.after["excerpt"] ?? "")}`;
    case "comment_update":
      return "Comment edited";
    case "note_create":
      return `Note created: ${String(e.after["title"] ?? "")}`;
    case "note_update":
      return `Note edited: ${String(e.after["title"] ?? e.before["title"] ?? "")}`;
    case "note_archive":
      return "Note archived";
    case "triage_verdict":
      return `Triage verdict: ${String(e.after["verdict"] ?? "")}`;
    case "issue_create":
      return `Issue created: ${String(e.after["title"] ?? "")}`;
    case "issue_update":
      return `Issue update: ${Object.keys(e.payload).join(", ")}`;
    default:
      return e.kind;
  }
}

/** Mirrors web `reasonKey` + issues.json `agent_effects.reasons.*`. */
export function skipReasonLabel(reason: string): string {
  switch (reason) {
    case "already_reversed":
      return "already reversed";
    case "not_reversible":
      return "not reversible";
    case "window_expired":
      return "window expired";
    case "not_applied":
      return "held write, nothing to reverse";
    default:
      return "reversal failed";
  }
}

/**
 * One line per web toast (`undone` / `skipped` / `breaker`), joined for a
 * single native alert. Empty when nothing happened.
 */
export function undoReportLines(r: UndoReport): string[] {
  const lines: string[] = [];
  if (r.reversed > 0) lines.push(`${r.reversed} change(s) reversed`);
  if (r.skipped.length > 0) {
    lines.push(
      `${r.skipped.length} change(s) skipped: ${skipReasonLabel(r.skipped[0]?.reason ?? "")}`,
    );
  }
  if (r.breaker.tripped) {
    lines.push(
      `Breaker tripped: the agent now runs in ${r.breaker.trust_mode} mode`,
    );
  }
  return lines;
}
