/**
 * Pure display helpers for the triage queue.
 *
 * Mirrors packages/views/triage/components/triage-page.tsx (`ITEM_STATES`,
 * `ageSecondsToIso`, `formatPayload`) and the English strings in
 * packages/views/locales/en/triage.json. Mobile is English-only today; when
 * mobile ships i18n, mirror that namespace structure.
 *
 * Behavioral parity (apps/mobile/CLAUDE.md): the state segments, their order,
 * and the payload rendering rule must match web — a user must not see a
 * different set of buckets depending on the client.
 */
import type { TriageItemPayload, TriageItemState } from "@multica/core/types";

/**
 * The four states the list endpoint serves, in web's order. `pending` is the
 * live queue; the other three are history — and `dismissed` is the only place
 * an item can be reopened from.
 */
export const TRIAGE_STATES: TriageItemState[] = [
  "pending",
  "accepted",
  "dismissed",
  "merged",
];

/** Mirrors triage.json `filter.*`. */
export const TRIAGE_STATE_LABEL: Record<TriageItemState, string> = {
  pending: "Pending",
  accepted: "Accepted",
  dismissed: "Dismissed",
  merged: "Merged",
};

/** A state the server may add later still needs a label. */
export function triageStateLabel(state: string): string {
  return TRIAGE_STATE_LABEL[state as TriageItemState] ?? state;
}

/**
 * Oldest-pending age in seconds → an ISO timestamp `timeAgo` can render.
 * Same trick web uses so both clients phrase the age identically.
 */
export function ageSecondsToIso(seconds: number): string {
  return new Date(Date.now() - seconds * 1000).toISOString();
}

/**
 * The captured trigger payload, pretty-printed. `null` when there is nothing
 * to show — the caller then distinguishes "truncated" from "no payload"
 * using `payload.truncated`, exactly like web.
 */
export function formatTriagePayload(
  payload: TriageItemPayload | undefined,
): string | null {
  const body = payload?.body;
  if (body && Object.keys(body).length > 0) {
    try {
      return JSON.stringify(body, null, 2);
    } catch {
      return null;
    }
  }
  return null;
}

/**
 * Empty-state copy per state. Web splits `pending` (queue is clear) from the
 * three history buckets (nothing here yet); mobile keeps that distinction so
 * "no accepted items" never reads as "the queue is clear".
 */
export function triageEmptyMessage(state: TriageItemState): string {
  if (state === "pending") {
    return "Queue is clear. Webhook deliveries from a source set to Gate, and action items from a recorded meeting, wait here until a human accepts or dismisses them.";
  }
  return "Nothing here yet.";
}
