/**
 * Pure display helpers for meetings.
 *
 * Mirrors packages/views/meetings/components/meetings-page.tsx
 * (`KNOWN_STATUSES` / `meetingStatusDotClass`) and
 * meeting-detail-page.tsx (`ACTION_STATES` / `actionStateLabel`), plus the
 * English strings in packages/views/locales/en/meetings.json.
 *
 * Both label functions fall back to the raw server value rather than dropping
 * it: apps/mobile/CLAUDE.md forbids silently swallowing a state enum the
 * backend added after this build shipped.
 */

/** Mirrors meetings.json `status.*`. */
const STATUS_LABEL: Record<string, string> = {
  recording: "Recording",
  summarizing: "Summarizing",
  done: "Done",
  failed: "Failed",
};

export function meetingStatusLabel(status: string): string {
  return STATUS_LABEL[status] ?? "Unknown";
}

/**
 * Tailwind class for the status dot. Same colour mapping as web's
 * `meetingStatusDotClass` so a recording meeting reads red on both clients.
 */
export function meetingStatusDotClass(status: string): string {
  switch (status) {
    case "recording":
      return "bg-red-500";
    case "summarizing":
      return "bg-blue-500";
    case "done":
      return "bg-emerald-500";
    case "failed":
      return "bg-red-500";
    default:
      return "bg-muted-foreground/40";
  }
}

/**
 * Mirrors meetings.json `action_state.*`. The list is wider than
 * `TriageItemState` on purpose: an action item's triage row can also be
 * superseded, expired or dropped, and web renders all seven.
 */
const ACTION_STATE_LABEL: Record<string, string> = {
  pending: "Pending",
  accepted: "Accepted",
  dismissed: "Dismissed",
  merged: "Merged",
  superseded: "Superseded",
  expired: "Expired",
  dropped: "Dropped",
};

export function meetingActionStateLabel(state: string): string {
  return ACTION_STATE_LABEL[state] ?? state;
}
