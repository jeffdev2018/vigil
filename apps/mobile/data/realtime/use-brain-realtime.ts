/**
 * Workspace Brain realtime.
 *
 * Two event families, one hook, because they share the `["brain", wsId]`
 * cache prefix:
 *
 *   - `brain_capture:changed` — a capture was created, transcribed,
 *     suggested, organized, merged, discarded, reopened or deleted
 *     (every `h.publishBrainCapture` in
 *     server/internal/handler/brain_capture.go, plus the delete publish).
 *   - `workspace_note:created|updated|deleted` — a member, an agent run or
 *     the daily curation pass wrote a note.
 *
 * Invalidate-not-patch, condition 1 of the patch-over-invalidate rule in
 * apps/mobile/CLAUDE.md: the capture payload is a change hint
 * (`{capture_id, status, change}`), not the row, and the projections it moves
 * — inbox ordering, `raw_count`, the note list's pinned-first ordering, its
 * tag facets, every cached ranked-search hit — are all computed server-side.
 *
 * The `change` field is what keeps this cheap: `captured`, `transcribed`,
 * `suggested`, `discarded`, `reopened` and `deleted` change nothing a note
 * query returns, so they clear the capture prefix only. `note` and `merge`
 * wrote a note, so they clear the whole Brain prefix. Same split as web's
 * `onBrainCaptureChanged` (packages/core/brain/ws-updaters.ts) — mirrored,
 * not imported, because the key factories are different runtime instances.
 *
 * Mounted listing-level in `_layout.tsx`, not per-screen: the raw-capture
 * badge in the More popover must stay fresh while the user is anywhere in
 * the workspace, and a capture arriving from the CLI, an MCP client, a chat
 * bot or an agent run has no per-record id to scope to.
 */
import { useQueryClient } from "@tanstack/react-query";
import {
  invalidateBrainCaptures,
  invalidateBrainNotes,
} from "@/data/mutations/brain";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";

/** The change values that also wrote a note (contract: `change` on
 *  `brain_capture:changed`). Anything else is capture-only. */
const NOTE_WRITING_CHANGES = new Set(["note", "merge"]);

export function useBrainRealtime() {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      const notes = () => invalidateBrainNotes(qc, wsId);
      const captures = () => invalidateBrainCaptures(qc, wsId);
      return [
        ws.on("brain_capture:changed", (payload) => {
          // The payload has no formal interface in WSEventPayloadMap, so it
          // arrives as `unknown` and has to be narrowed here rather than
          // cast — an absent/odd `change` falls back to the safe branch
          // (refresh notes too) instead of silently skipping a note refresh.
          const change = (payload as { change?: unknown } | undefined)?.change;
          if (typeof change === "string" && !NOTE_WRITING_CHANGES.has(change)) {
            captures();
            return;
          }
          notes();
        }),
        ws.on("workspace_note:created", notes),
        ws.on("workspace_note:updated", notes),
        ws.on("workspace_note:deleted", notes),
        // Reconnect: we may have missed events while disconnected, and this
        // hook owns exactly the `["brain", wsId]` prefix — no global sweep.
        ws.onReconnect(notes),
      ];
    },
    [qc],
  );
}
