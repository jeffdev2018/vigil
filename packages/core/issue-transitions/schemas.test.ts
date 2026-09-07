// @vitest-environment node
import { describe, expect, it } from "vitest";
import { IssueTransitionPendingError } from "../api/client";
import {
  EffectiveTransitionsSchema,
  isTransitionPending,
  IssueTransitionRequestListSchema,
  IssueTransitionRuleListSchema,
  PendingTransitionSchema,
  effectiveFor,
  pendingTransitionRequest,
} from "./schemas";

// Canonical parsing + helper matrix for F28's client shapes. The component
// suites assert wiring, not this grid.

describe("IssueTransitionRuleListSchema", () => {
  it("keeps a well-formed rule", () => {
    const parsed = IssueTransitionRuleListSchema.parse({
      rules: [{
        id: "r1",
        workspace_id: "ws",
        project_id: null,
        from_category: "in_progress",
        to_category: "done",
        allowed_roles: ["admin"],
        allow_actor_types: ["agent"],
        requires_approval: true,
        approver_roles: ["owner"],
        reject_status_key: "blocked",
        enabled: true,
        actors: [{ actor_type: "agent", actor_id: "a1" }],
        created_at: "2026-01-01T00:00:00Z",
        updated_at: "2026-01-01T00:00:00Z",
      }],
      categories: ["todo", "done"],
    });
    expect(parsed.rules[0]?.to_category).toBe("done");
    expect(parsed.rules[0]?.actors[0]?.actor_id).toBe("a1");
  });

  it("survives a backend that drops or retypes every optional field", () => {
    const parsed = IssueTransitionRuleListSchema.parse({
      rules: [{ id: "r1", allowed_roles: "admin", requires_approval: "yes", actors: 3 }],
    });
    expect(parsed.rules[0]?.allowed_roles).toEqual([]);
    expect(parsed.rules[0]?.requires_approval).toBe(false);
    expect(parsed.rules[0]?.actors).toEqual([]);
    expect(parsed.categories).toEqual([]);
  });

  it("fails a payload with no rules array of the right shape at the top level", () => {
    // `id` is the one field with no .catch(): a rule without an identity is
    // not a rule, and silently inventing one would put an unaddressable row in
    // the editor.
    expect(IssueTransitionRuleListSchema.parse({ rules: [{ to_category: "done" }] }).rules)
      .toEqual([]);
  });
});

describe("EffectiveTransitionsSchema", () => {
  it("defaults a malformed `allowed` to true", () => {
    // Silence is permission everywhere in F28. A parse failure must never grey
    // out an option the server would have accepted.
    const parsed = EffectiveTransitionsSchema.parse({
      transitions: [{ to_category: "done", allowed: "maybe" }],
    });
    expect(parsed.transitions[0]?.allowed).toBe(true);
  });

  it("falls back to an empty list on a malformed envelope", () => {
    const parsed = EffectiveTransitionsSchema.parse({ transitions: "nope" });
    expect(parsed.transitions).toEqual([]);
  });
});

describe("effectiveFor", () => {
  const effective = EffectiveTransitionsSchema.parse({
    issue_id: "i1",
    from_category: "todo",
    transitions: [
      { to_category: "done", allowed: false, requires_approval: false, reason: "no_grant", rule_id: "r1" },
      { to_category: "in_review", allowed: true, requires_approval: true, reason: "requires_approval" },
    ],
  });

  it("reads a category the server answered for", () => {
    expect(effectiveFor(effective, "done").allowed).toBe(false);
    expect(effectiveFor(effective, "in_review").requires_approval).toBe(true);
  });

  it("allows a category the server did not mention", () => {
    expect(effectiveFor(effective, "cancelled").allowed).toBe(true);
  });

  it("allows everything when the query has not resolved yet", () => {
    expect(effectiveFor(undefined, "done")).toMatchObject({ allowed: true, requires_approval: false });
  });
});

describe("pendingTransitionRequest", () => {
  const list = IssueTransitionRequestListSchema.parse({
    requests: [
      { id: "q2", state: "rejected", to_status: "done" },
      { id: "q1", state: "pending", to_status: "done" },
    ],
  });

  it("finds the one pending request", () => {
    expect(pendingTransitionRequest(list)?.id).toBe("q1");
  });

  it("returns undefined when nothing is pending", () => {
    expect(pendingTransitionRequest(IssueTransitionRequestListSchema.parse({ requests: [] })))
      .toBeUndefined();
  });

  it("returns undefined for an unresolved query", () => {
    expect(pendingTransitionRequest(undefined)).toBeUndefined();
  });
});

describe("isTransitionPending", () => {
  it("recognises the client's held-transition error across a module boundary", () => {
    // Matches on `name` rather than instanceof, so a partially-mocked
    // @multica/core/api in a component suite still classifies it correctly.
    expect(isTransitionPending(new IssueTransitionPendingError("q1"))).toBe(true);
    const lookalike = new Error("held");
    lookalike.name = "IssueTransitionPendingError";
    expect(isTransitionPending(lookalike)).toBe(true);
  });

  it("does not classify an ordinary failure as pending", () => {
    expect(isTransitionPending(new Error("boom"))).toBe(false);
    expect(isTransitionPending({ name: "IssueTransitionPendingError" })).toBe(false);
    expect(isTransitionPending(undefined)).toBe(false);
  });
});

describe("PendingTransitionSchema", () => {
  it("recognises the 202 body", () => {
    expect(PendingTransitionSchema.safeParse({ status: "pending_approval", request_id: "q1" }).success)
      .toBe(true);
  });

  it("does not mistake an applied issue for a held one", () => {
    // The success body of an ordinary update carries `status: "done"`; if this
    // matched, every successful status write would be reported as pending.
    expect(PendingTransitionSchema.safeParse({ id: "i1", status: "done" }).success).toBe(false);
    expect(PendingTransitionSchema.safeParse({ status: "pending_approval" }).success).toBe(false);
  });
});
