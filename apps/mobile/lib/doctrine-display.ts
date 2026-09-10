/**
 * Workspace doctrine display helpers (OS plan, chantier 22).
 *
 * Parity target is `server/internal/handler/workspace_doctrine.go` — there
 * is no packages/views doctrine page to mirror yet, so the labels, the
 * enums and above all the review permission come from the handler. Keep
 * `canReviewDoctrineVersion` in step with `reviewDoctrineVersion` there: a
 * screen that offers Approve where the server answers 403 is exactly the
 * kind of permission divergence apps/mobile/CLAUDE.md forbids.
 */

/** Report kinds accepted by POST /reports (doctrineReportKinds). */
export const DOCTRINE_REPORT_KINDS = [
  "conflict",
  "refusal",
  "ambiguity",
] as const;

const REPORT_KIND_LABEL: Record<string, string> = {
  conflict: "Conflict",
  refusal: "Refusal",
  ambiguity: "Ambiguity",
};

/** Unknown kinds render as-is rather than disappearing (root CLAUDE.md
 *  "Server-driven enum switches need a default branch"). */
export function doctrineReportKindLabel(kind: string): string {
  return REPORT_KIND_LABEL[kind] ?? kind;
}

const REPORT_STATUS_LABEL: Record<string, string> = {
  open: "Open",
  acknowledged: "Acknowledged",
  dismissed: "Dismissed",
};

export function doctrineReportStatusLabel(status: string): string {
  return REPORT_STATUS_LABEL[status] ?? status;
}

const VERSION_STATUS_LABEL: Record<string, string> = {
  active: "Live",
  pending: "Awaiting review",
  rejected: "Rejected",
  superseded: "Superseded",
};

export function doctrineVersionStatusLabel(status: string): string {
  return VERSION_STATUS_LABEL[status] ?? status;
}

/**
 * Who may approve or reject a pending proposal. Mirrors
 * `reviewDoctrineVersion` in the handler, in its order:
 *
 *   1. owners and admins only — the `can_publish` flag on GET /doctrine is
 *      the same `isWorkspaceManager` check, so mobile reads it rather than
 *      re-deriving the role;
 *   2. the version must still be `pending`;
 *   3. the author does not review their own proposal — unless no other
 *      owner/admin exists, in which case the handler lets them through
 *      (otherwise a single-manager workspace could never publish).
 */
export function canReviewDoctrineVersion(input: {
  canPublish: boolean;
  status: string;
  authorId: string | null;
  userId: string | null;
  /** Owners/admins of the workspace other than the current user. */
  otherManagerCount: number;
}): boolean {
  if (!input.canPublish) return false;
  if (input.status !== "pending") return false;
  // Without a known viewer we cannot tell self-review from peer review;
  // withholding the action is the safe side (the server would 403).
  if (!input.userId) return false;
  if (input.authorId && input.authorId === input.userId) {
    return input.otherManagerCount === 0;
  }
  return true;
}

/** Owners/admins other than `userId` — the count clause of rule 3 above.
 *  `isWorkspaceManager` in the handler is `role === owner || admin`. */
export function countOtherManagers(
  members: { user_id: string; role: string }[],
  userId: string | null,
): number {
  return members.filter(
    (m) =>
      (m.role === "owner" || m.role === "admin") && m.user_id !== userId,
  ).length;
}
