/**
 * Workspace Brain writes — notes and the capture inbox.
 *
 * Nothing here is optimistic, and the reasons are the same as web's
 * (`packages/core/brain/mutations.ts`):
 *
 *   - A note write bumps `revision`, and the server owns it. Guessing the
 *     next revision locally would hand the next PATCH a token the server
 *     never issued, turning the conflict guard into a lie.
 *   - A capture write is a server decision: `organize` may create a note,
 *     append to one, or neither, and the ordering / `raw_count` / tag facets
 *     it moves are all computed server-side.
 *   - Organize / discard / delete all navigate away, and the root CLAUDE.md
 *     gate says a flow that navigates awaits the server first.
 *
 * A capture *create* is the one exception to "await before showing": the
 * composer renders the pending capture immediately with a visible pending
 * state and retries on failure (the chat pending-message pattern), rather
 * than optimistically writing a row into the cache. That state lives in the
 * composer, not here — this file only owns the request and the invalidation.
 */
import { useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import type {
  CreateBrainCaptureInput,
  CreateWorkspaceNoteInput,
  OrganizeBrainCaptureInput,
  UpdateWorkspaceNoteInput,
} from "@multica/core/types";
import { api, type FileAsset } from "@/data/api";
import { brainCaptureKeys, brainKeys } from "@/data/queries/brain";
import { useWorkspaceStore } from "@/data/workspace-store";
import { apiErrorMessage } from "@/lib/issue-goal-display";

/**
 * Read an HTTP status off a thrown error structurally rather than through
 * `instanceof ApiError`. Same idiom as data/mutations/delivery.ts, and it
 * keeps these helpers testable without loading the native fetch chain.
 */
function httpStatus(err: unknown): number | undefined {
  if (
    err &&
    typeof err === "object" &&
    "status" in err &&
    typeof (err as { status: unknown }).status === "number"
  ) {
    return (err as { status: number }).status;
  }
  return undefined;
}

/** `[Alert title, Alert body]`, ready to spread into `Alert.alert`. */
export type FailureAlert = [string, string];

/**
 * A 409 on organize means the capture left "raw" while the screen was open —
 * someone else, or an agent, organized it first. Retrying would fail the same
 * way, so say what happened instead of offering a generic failure. A 400 on
 * merge is the size ceiling: the note would pass 20000 characters.
 */
export function organizeFailure(err: unknown, action: string): FailureAlert {
  const status = httpStatus(err);
  if (status === 409) {
    return [
      "Already organized",
      "Someone got to this capture first. Pull to refresh the inbox to see where it went.",
    ];
  }
  if (status === 404) {
    return [
      "Note not found",
      "The note you picked is no longer in this workspace. Pick another one, or save the capture as a new note.",
    ];
  }
  return [
    `Could not ${action} the capture`,
    apiErrorMessage(err, "Try again in a moment."),
  ];
}

/**
 * A 503 from `suggest` is not an error the user caused: the workspace has no
 * model wired up, and the answer is to organize it by hand. A 502 is the
 * model failing, which is worth retrying.
 */
export function suggestFailure(err: unknown): FailureAlert {
  const status = httpStatus(err);
  if (status === 503) {
    return [
      "No model configured",
      "Nothing is set up to read captures yet, so organize this one yourself.",
    ];
  }
  if (status === 502) {
    return [
      "The model could not answer",
      apiErrorMessage(err, "Try again in a moment."),
    ];
  }
  return [
    "Could not ask for a suggestion",
    apiErrorMessage(err, "Try again in a moment."),
  ];
}

/** Reopen refuses anything that is not discarded (409). */
export function reopenFailure(err: unknown): FailureAlert {
  if (httpStatus(err) === 409) {
    return [
      "Already back in the inbox",
      "Someone reopened this capture before you did.",
    ];
  }
  return [
    "Could not reopen the capture",
    apiErrorMessage(err, "Try again in a moment."),
  ];
}

/**
 * Note writes. A 409 is the revision guard: someone (or the daily curation
 * pass) wrote first, so the edit must be re-applied on top of theirs. A 403
 * is the delete rule — only a workspace admin or the note's author may
 * remove one (server canDeleteWorkspaceNote) — and the server's own sentence
 * is the clearest thing to show.
 */
export function noteWriteFailure(err: unknown, action: string): FailureAlert {
  const status = httpStatus(err);
  if (status === 409) {
    return [
      "Changed while you were editing",
      "Someone else (or the curation pass) saved this note first. Reload it and re-apply your change.",
    ];
  }
  if (status === 403) {
    return [
      "Not allowed",
      apiErrorMessage(
        err,
        "Only a workspace admin or the note's author can delete this note.",
      ),
    ];
  }
  return [`Could not ${action} the note`, apiErrorMessage(err, "Try again in a moment.")];
}

/** Notes moved: the list ordering (pinned first, then updated_at), the tag
 *  facets and every ranked-search hit are all server-owned. */
export function invalidateBrainNotes(qc: QueryClient, wsId: string | null): void {
  qc.invalidateQueries({ queryKey: brainKeys.all(wsId) });
}

/** Only the inbox moved — a note query returns nothing different. */
export function invalidateBrainCaptures(
  qc: QueryClient,
  wsId: string | null,
): void {
  qc.invalidateQueries({ queryKey: brainCaptureKeys.captures(wsId) });
}

function useBrainInvalidate() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return {
    notes: () => invalidateBrainNotes(qc, wsId),
    captures: () => invalidateBrainCaptures(qc, wsId),
  };
}

// --- Notes ---

export function useCreateWorkspaceNote() {
  const invalidate = useBrainInvalidate();
  return useMutation({
    mutationFn: (input: CreateWorkspaceNoteInput) =>
      api.createWorkspaceNote(input),
    onSettled: invalidate.notes,
  });
}

export function useUpdateWorkspaceNote() {
  const invalidate = useBrainInvalidate();
  return useMutation({
    mutationFn: (v: { id: string; input: UpdateWorkspaceNoteInput }) =>
      api.updateWorkspaceNote(v.id, v.input),
    onSettled: invalidate.notes,
  });
}

export function useSetWorkspaceNoteArchived() {
  const invalidate = useBrainInvalidate();
  return useMutation({
    mutationFn: (v: { id: string; archived: boolean }) =>
      api.setWorkspaceNoteArchived(v.id, v.archived),
    onSettled: invalidate.notes,
  });
}

export function useDeleteWorkspaceNote() {
  const invalidate = useBrainInvalidate();
  return useMutation({
    mutationFn: (id: string) => api.deleteWorkspaceNote(id),
    onSettled: invalidate.notes,
  });
}

// --- Captures ---

export function useCreateBrainCapture() {
  const invalidate = useBrainInvalidate();
  return useMutation({
    mutationFn: (input: CreateBrainCaptureInput) =>
      api.createBrainCapture(input),
    onSettled: invalidate.captures,
  });
}

/** A photo, a voice memo or any file. `origin: "mobile"` is set by api.ts. */
export function useUploadBrainCapture() {
  const invalidate = useBrainInvalidate();
  return useMutation({
    mutationFn: (v: {
      asset: FileAsset;
      content?: string;
      title_hint?: string;
    }) =>
      api.uploadBrainCapture(v.asset, {
        content: v.content,
        title_hint: v.title_hint,
      }),
    onSettled: invalidate.captures,
  });
}

export function useSuggestBrainCapture() {
  const invalidate = useBrainInvalidate();
  return useMutation({
    mutationFn: (id: string) => api.suggestBrainCapture(id),
    onSettled: invalidate.captures,
  });
}

/**
 * `note` and `merge` also write a note, which reshuffles the note list, its
 * tag facets and every cached search hit — so those invalidate the whole
 * Brain prefix. `discard` touches nothing a note query returns.
 */
export function useOrganizeBrainCapture() {
  const invalidate = useBrainInvalidate();
  return useMutation({
    mutationFn: (v: { id: string; input: OrganizeBrainCaptureInput }) =>
      api.organizeBrainCapture(v.id, v.input),
    onSettled: (_data, _err, v) => {
      if (v.input.action === "discard") invalidate.captures();
      else invalidate.notes();
    },
  });
}

export function useReopenBrainCapture() {
  const invalidate = useBrainInvalidate();
  return useMutation({
    mutationFn: (id: string) => api.reopenBrainCapture(id),
    onSettled: invalidate.captures,
  });
}

/** Gone for good. The note an organized capture produced is untouched, so
 *  only the capture projection moves. */
export function useDeleteBrainCapture() {
  const invalidate = useBrainInvalidate();
  return useMutation({
    mutationFn: (id: string) => api.deleteBrainCapture(id),
    onSettled: invalidate.captures,
  });
}
