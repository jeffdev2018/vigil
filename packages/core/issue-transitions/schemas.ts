import { z } from "zod";

// Transition rules and approval gates (F28). A rule says who may move an issue
// from one status CATEGORY into another, and whether that move waits for an
// approver. Categories, not status keys: renaming a status or adding a custom
// one in the same category must not void the rule written against it.

export const TRANSITION_ACTOR_TYPES = ["member", "agent", "squad"] as const;
export type TransitionActorType = (typeof TRANSITION_ACTOR_TYPES)[number];

export const TransitionRuleActorSchema = z.object({
  actor_type: z.string().catch("member"),
  actor_id: z.string().catch(""),
}).loose();
export type TransitionRuleActor = z.infer<typeof TransitionRuleActorSchema>;

export const IssueTransitionRuleSchema = z.object({
  id: z.string(),
  workspace_id: z.string().catch(""),
  project_id: z.string().nullish().catch(null),
  from_category: z.string().nullish().catch(null),
  to_category: z.string().catch(""),
  allowed_roles: z.array(z.string()).catch([]).default([]),
  allow_actor_types: z.array(z.string()).catch([]).default([]),
  requires_approval: z.boolean().catch(false),
  approver_roles: z.array(z.string()).catch([]).default([]),
  reject_status_key: z.string().nullish().catch(null),
  enabled: z.boolean().catch(true),
  actors: z.array(TransitionRuleActorSchema).catch([]).default([]),
  created_at: z.string().catch(""),
  updated_at: z.string().catch(""),
}).loose();
export type IssueTransitionRule = z.infer<typeof IssueTransitionRuleSchema>;

export const EMPTY_TRANSITION_RULE: IssueTransitionRule = {
  id: "",
  workspace_id: "",
  project_id: null,
  from_category: null,
  to_category: "",
  allowed_roles: [],
  allow_actor_types: [],
  requires_approval: false,
  approver_roles: [],
  reject_status_key: null,
  enabled: true,
  actors: [],
  created_at: "",
  updated_at: "",
};

export const IssueTransitionRuleListSchema = z.object({
  rules: z.array(IssueTransitionRuleSchema).catch([]).default([]),
  categories: z.array(z.string()).catch([]).default([]),
}).loose();
export type IssueTransitionRuleList = z.infer<typeof IssueTransitionRuleListSchema>;

export const EMPTY_TRANSITION_RULES: IssueTransitionRuleList = { rules: [], categories: [] };

// One target category's answer for one issue and one viewer.
export const EffectiveTransitionSchema = z.object({
  to_category: z.string().catch(""),
  // Defaults to TRUE on a malformed row: silence is permission everywhere else
  // in this feature, and a parse failure must not grey out a picker option the
  // server would have accepted.
  allowed: z.boolean().catch(true),
  requires_approval: z.boolean().catch(false),
  reason: z.string().catch(""),
  rule_id: z.string().nullish().catch(null),
}).loose();
export type EffectiveTransition = z.infer<typeof EffectiveTransitionSchema>;

export const EffectiveTransitionsSchema = z.object({
  issue_id: z.string().catch(""),
  from_category: z.string().catch(""),
  transitions: z.array(EffectiveTransitionSchema).catch([]).default([]),
}).loose();
export type EffectiveTransitions = z.infer<typeof EffectiveTransitionsSchema>;

export const EMPTY_EFFECTIVE_TRANSITIONS: EffectiveTransitions = {
  issue_id: "",
  from_category: "",
  transitions: [],
};

export const IssueTransitionRequestSchema = z.object({
  id: z.string(),
  workspace_id: z.string().catch(""),
  issue_id: z.string().catch(""),
  from_status: z.string().catch(""),
  to_status: z.string().catch(""),
  rule_id: z.string().nullish().catch(null),
  requested_by_type: z.string().catch("member"),
  requested_by_id: z.string().catch(""),
  state: z.string().catch("pending"),
  decided_by_type: z.string().nullish().catch(null),
  decided_by_id: z.string().nullish().catch(null),
  decided_at: z.string().nullish().catch(null),
  note: z.string().nullish().catch(null),
  created_at: z.string().catch(""),
}).loose();
export type IssueTransitionRequest = z.infer<typeof IssueTransitionRequestSchema>;

export const IssueTransitionRequestListSchema = z.object({
  requests: z.array(IssueTransitionRequestSchema).catch([]).default([]),
}).loose();
export type IssueTransitionRequestList = z.infer<typeof IssueTransitionRequestListSchema>;

export const EMPTY_TRANSITION_REQUESTS: IssueTransitionRequestList = { requests: [] };

// The 202 body: the write was recorded, not applied.
export const PendingTransitionSchema = z.object({
  status: z.literal("pending_approval"),
  request_id: z.string(),
}).loose();

/**
 * Whether an error is the client's translation of the 202 held-transition
 * answer (IssueTransitionPendingError, raised in the API client).
 *
 * Matches on `name`, not `instanceof`. The class lives in `@multica/core/api`,
 * which ~100 component suites replace with a partial `vi.mock`; an `instanceof`
 * check would make every one of them fail on an export they never asked for.
 * The name is set explicitly in the constructor, so this is exact.
 */
export function isTransitionPending(err: unknown): boolean {
  return err instanceof Error && err.name === "IssueTransitionPendingError";
}

/** The one pending request on an issue, or undefined when there is none. */
export function pendingTransitionRequest(
  list: IssueTransitionRequestList | undefined,
): IssueTransitionRequest | undefined {
  return list?.requests?.find((r) => r.state === "pending");
}

/**
 * The effective answer for one target category, defaulting to "allowed" for a
 * category the server did not mention — the same silence-is-permission rule
 * the resolver applies.
 */
export function effectiveFor(
  effective: EffectiveTransitions | undefined,
  category: string,
): EffectiveTransition {
  return (
    effective?.transitions?.find((t) => t.to_category === category) ?? {
      to_category: category,
      allowed: true,
      requires_approval: false,
      reason: "",
      rule_id: null,
    }
  );
}
