/**
 * Tap-to-answer routing for push notifications (K64 / JEF-244).
 *
 * Pure payload → route mapping, no imports, so the vitest node environment
 * exercises it without mocking Expo modules.
 *
 * Payload keys confirmed from the server senders:
 *   - Decision Card ask (server/internal/handler/attention_inbox.go,
 *     pushToUsers in push_token.go):
 *     data = { kind: "decision_request", issue_id, decision_id }
 *     — note: NO workspace key; the current workspace is the only sensible
 *     target.
 *   - Morning briefing (server/internal/handler/briefing_digest.go):
 *     data = { kind: "morning_briefing", workspace_id }  (a UUID, not a slug —
 *     resolved to a slug against the user's workspace list).
 *
 * Safety contract: never crash, never navigate to a bogus route. Anything
 * unrecognized returns null (no-op) or the current workspace's inbox tab.
 */

/** The push kinds that land on the Decision Cards screen. */
const DECISION_KINDS = new Set(["decision_request", "morning_briefing"]);

export interface NotificationRouteContext {
  /** Slug of the workspace the user is currently in (route-driven store). */
  currentSlug: string | null;
  /** The user's workspace memberships, for workspace_id → slug resolution.
   *  Undefined when the list hasn't loaded yet (cold start). */
  workspaces?: readonly { id: string; slug: string }[];
}

const decisionsRoute = (slug: string) => `/${slug}/inbox/decisions`;
const inboxRoute = (slug: string) => `/${slug}/inbox`;

/**
 * Maps a push notification's `data` payload to an app route.
 *
 *   known kind + workspace pinned & resolvable → that workspace's decisions
 *   known kind + no workspace key              → current workspace's decisions
 *   known kind + workspace pinned but unknown  → current workspace's inbox tab
 *   nothing resolvable                         → null (no-op)
 *   unknown / missing / malformed kind         → null (no-op)
 */
export function routeForNotification(
  data: unknown,
  ctx: NotificationRouteContext,
): string | null {
  if (!data || typeof data !== "object") return null;
  const d = data as Record<string, unknown>;
  if (typeof d.kind !== "string" || !DECISION_KINDS.has(d.kind)) return null;

  const workspaceId = typeof d.workspace_id === "string" ? d.workspace_id : null;
  // workspace_slug / workspace aren't sent today; accepted so a future server
  // payload pins the workspace without a client update.
  const workspaceSlug =
    typeof d.workspace_slug === "string"
      ? d.workspace_slug
      : typeof d.workspace === "string"
        ? d.workspace
        : null;

  if (workspaceId) {
    const matched = ctx.workspaces?.find((w) => w.id === workspaceId);
    if (matched) return decisionsRoute(matched.slug);
    // Pinned to a workspace we can't resolve (not a member, or the list
    // hasn't loaded): fall back to the current workspace's inbox tab rather
    // than guessing a decisions screen in the wrong workspace.
    return ctx.currentSlug ? inboxRoute(ctx.currentSlug) : null;
  }
  if (workspaceSlug) {
    // When memberships are known, only accept a slug that is one — anything
    // else would route into a workspace redirect loop.
    if (!ctx.workspaces || ctx.workspaces.some((w) => w.slug === workspaceSlug)) {
      return decisionsRoute(workspaceSlug);
    }
    return ctx.currentSlug ? inboxRoute(ctx.currentSlug) : null;
  }

  // No workspace pinned (today's decision_request payload): the current
  // workspace is where the badge counts these cards, so it's the target.
  return ctx.currentSlug ? decisionsRoute(ctx.currentSlug) : null;
}
