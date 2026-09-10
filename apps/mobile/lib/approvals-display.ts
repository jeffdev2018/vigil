/**
 * Pure helpers for the inline approvals feed (GET /api/approvals): the
 * countdown, the gate-details projection, English kind labels, timeline
 * interleaving and inbox-item matching that
 * `components/approvals/approval-card.tsx` and its mount points build on.
 *
 * `approvalSecondsLeft` / `formatCountdown` / `gateDetails` are copied from
 * `packages/core/approvals/schemas.ts` rather than imported — see the
 * "Inline approvals" comment in `data/schemas.ts` for why
 * `@multica/core/approvals` is not on the mobile import whitelist. Keep the
 * logic identical to the core source; if either changes, mirror the other
 * by hand.
 */
import type { InboxItem } from "@multica/core/types";
import type { ApprovalGate, ApprovalItem, ApprovalSource } from "@/data/schemas";
import type { TimelineRow } from "@/lib/timeline-thread";

/** The ask's deadline: the gate's expiry first, else the decision SLA. */
export function approvalDeadline(
  item: Pick<ApprovalItem, "expires_at" | "sla_deadline_at">,
): Date | null {
  const raw = item.expires_at ?? item.sla_deadline_at;
  if (!raw) return null;
  const d = new Date(raw);
  return Number.isNaN(d.getTime()) ? null : d;
}

/** Whole seconds left before the deadline, 0 when passed, null when none. */
export function approvalSecondsLeft(
  item: Pick<ApprovalItem, "expires_at" | "sla_deadline_at">,
  now: Date = new Date(),
): number | null {
  const deadline = approvalDeadline(item);
  if (!deadline) return null;
  return Math.max(0, Math.floor((deadline.getTime() - now.getTime()) / 1000));
}

/** Compact "12m" / "3h 05m" / "2d" countdown label; "" when none. */
export function formatCountdown(seconds: number | null): string {
  if (seconds === null) return "";
  if (seconds <= 0) return "0m";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ${String(minutes % 60).padStart(2, "0")}m`;
  return `${Math.floor(hours / 24)}d`;
}

/** Gate arguments and paths the card shows, pulled out of the details bag. */
export function gateDetails(gate: ApprovalGate | null | undefined): {
  params: unknown;
  paths: string[];
  blastRadius: string;
  requiredApprovals: number;
  approvals: number;
} {
  const d = gate?.details ?? {};
  const paths = Array.isArray(d.paths)
    ? d.paths.filter((p): p is string => typeof p === "string")
    : [];
  const approvers = Array.isArray(d.approvers) ? d.approvers.length : 0;
  return {
    params: d.params ?? null,
    paths,
    blastRadius: typeof d.blast_radius === "string" ? d.blast_radius : "",
    requiredApprovals:
      typeof d.required_approvals === "number" ? d.required_approvals : 1,
    approvals: approvers,
  };
}

/**
 * English label for the card header. Mirrors the `approvalKindLabel`
 * switch in packages/views/approvals/approval-card.tsx and the English
 * copy in packages/views/locales/en/issues.json `approvals.kind_*` (mobile
 * v1 is English-only — see lib/time-ago.ts).
 */
export function approvalKindLabel(
  approval: Pick<ApprovalItem, "kind" | "gate">,
): string {
  switch (approval.kind) {
    case "gate": {
      const type = approval.gate?.gate_type ?? "";
      if (type === "git_push") return "Git push held";
      if (type === "spend") return "Spend held";
      return "Tool call held";
    }
    case "plan":
      return "Plan approval";
    case "interview":
      return "Interview question";
    case "preview":
      return "Show me first";
    case "watchdog":
      return "Watchdog verdict";
    case "pipeline":
      return "Pipeline stage";
    case "goal_attach":
      return "Goal attachment";
    case "org_assign":
      return "Routed assignment";
    // Proposed autopilot (JEF-373): a paused automation waiting for someone
    // to activate or discard it. The feed classifies it by the "autopilot:"
    // option prefix, the way it already does for calendar proposals.
    case "autopilot_proposal":
      return "Autopilot proposal";
    case "transition":
      return "Status change held";
    case "goal_question":
      return "Question from the run";
    default:
      return "Decision";
  }
}

/** Short label for the pending-asks bar chip. Mirrors web's `approvalShortLabel`. */
export function approvalShortLabel(
  a: Pick<ApprovalItem, "source" | "transition" | "question">,
): string {
  if (a.source === "transition" && a.transition) {
    return `${a.transition.from_status} → ${a.transition.to_status}`;
  }
  const q = a.question.replace(/^Blocked action · /, "");
  return q.length > 48 ? q.slice(0, 47) + "…" : q;
}

/** Sentinel id prefix for a synthetic TimelineRow standing in for a pending
 *  ask — same technique timeline-list.tsx already uses for the "New"
 *  divider row (DIVIDER_ID). */
export const APPROVAL_ROW_PREFIX = "__approval__:";

export function approvalRowId(approvalId: string): string {
  return `${APPROVAL_ROW_PREFIX}${approvalId}`;
}

export function approvalIdOfRowId(rowId: string): string | null {
  return rowId.startsWith(APPROVAL_ROW_PREFIX)
    ? rowId.slice(APPROVAL_ROW_PREFIX.length)
    : null;
}

function approvalTimelineRow(approval: ApprovalItem): TimelineRow {
  return {
    entry: {
      id: approvalRowId(approval.id),
      type: "activity",
      actor_type: approval.asked_by?.type ?? "",
      actor_id: approval.asked_by?.id ?? "",
      created_at: approval.created_at,
    },
    replies: [],
  };
}

/**
 * Interleaves pending asks into the timeline by time asked, same design as
 * `interleaveApprovals` in packages/views/issues/components/issue-detail.tsx
 * (not exported from core — it lives inline there — so this is a from-
 * scratch port over mobile's flat `TimelineRow[]` shape rather than web's
 * grouped-entries shape). An ask with no usable timestamp sorts last, where
 * the eye looks for what is waiting now.
 */
export function interleaveApprovals(
  rows: TimelineRow[],
  approvals: ReadonlyArray<ApprovalItem>,
): TimelineRow[] {
  if (approvals.length === 0) return rows;
  const stamp = (created_at: string): number => {
    const t = Date.parse(created_at);
    return Number.isNaN(t) ? Number.POSITIVE_INFINITY : t;
  };
  const merged = [...rows, ...approvals.map(approvalTimelineRow)];
  return merged
    .map((row, i) => ({ row, i, t: stamp(row.entry.created_at) }))
    .sort((a, b) => a.t - b.t || a.i - b.i)
    .map(({ row }) => row);
}

/**
 * Matches a `decision_request` / `decision_escalated` /
 * `transition_approval_requested` / `goal_question` inbox item to its live
 * entry in the approvals feed, so the inbox detail screen can render the
 * same decidable card the timeline shows. Tries the id the server files in
 * `item.details` first (`decision_id` / `request_id` / `goal_id` — see
 * server/internal/handler/attention_inbox.go, issue_transition_gate.go,
 * goal_loop.go raiseQuestion), then falls back to issue_id + source, since
 * only one ask of a given source is ever open on an issue at a time.
 * Returns null when the ask was already decided (it left the feed) or the
 * item type has no approval counterpart.
 */
export function matchApprovalForInboxItem(
  item: Pick<InboxItem, "type" | "issue_id" | "details">,
  approvals: ReadonlyArray<ApprovalItem>,
): ApprovalItem | null {
  const details = item.details ?? {};
  let source: ApprovalSource;
  let detailId: string | undefined;
  switch (item.type) {
    case "decision_request":
    case "decision_escalated":
      source = "decision";
      detailId = details.decision_id;
      break;
    case "transition_approval_requested":
      source = "transition";
      detailId = details.request_id;
      break;
    case "goal_question":
      source = "goal_question";
      detailId = details.goal_id;
      break;
    default:
      return null;
  }
  if (detailId) {
    const byId = approvals.find(
      (a) => a.source === source && a.id === detailId,
    );
    if (byId) return byId;
  }
  return (
    approvals.find(
      (a) => a.source === source && a.issue.id === item.issue_id,
    ) ?? null
  );
}
