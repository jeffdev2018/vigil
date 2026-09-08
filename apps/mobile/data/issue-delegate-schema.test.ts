import { describe, expect, it } from "vitest";
import { IssueSchema } from "@multica/core/api/schemas";
import { EMPTY_ISSUE_FALLBACK } from "./schemas";

/**
 * Mobile's CLIENT-SIDE parsing of the delegate pair (F01) on GET /api/issues/:id.
 *
 * Scope, stated as precisely as inbox-schema.test.ts states its own: these are
 * hand-written fixtures against the core `IssueSchema` mobile reuses. They pin
 * how THIS client reacts to a payload; nothing here executes server code.
 *
 * Why it earns a test rather than riding on the core suite: mobile ships on a
 * different release cadence from the server, so a phone running an older build
 * against a newer backend — and a phone running a NEWER build against an older
 * one — are both routine here in a way they are not on web, where the bundle
 * arrives with the deploy. The second case is the dangerous one: `getIssue`
 * parses through `parseWithFallback`, so a schema that required the delegate
 * would turn every issue on an older backend into EMPTY_ISSUE_FALLBACK, whose
 * `id: ""` the detail screen renders as "issue not found".
 */

const serverIssue = {
  id: "issue-1",
  workspace_id: "ws-1",
  number: 7,
  identifier: "MUL-7",
  title: "Ship the delegate",
  description: null,
  status: "todo",
  priority: "medium",
  assignee_type: "member",
  assignee_id: "user-1",
  creator_type: "member",
  creator_id: "user-1",
  parent_issue_id: null,
  project_id: null,
  position: 0,
  stage: null,
  start_date: null,
  due_date: null,
  metadata: {},
  created_at: "2026-09-01T00:00:00Z",
  updated_at: "2026-09-01T00:00:00Z",
};

describe("issue delegate schema (mobile)", () => {
  it("parses the pair a current server sends", () => {
    const parsed = IssueSchema.safeParse({
      ...serverIssue,
      delegate_type: "member",
      delegate_id: "user-2",
    });
    expect(parsed.success).toBe(true);
    expect(parsed.success && parsed.data.delegate_type).toBe("member");
    expect(parsed.success && parsed.data.delegate_id).toBe("user-2");
  });

  it("parses an issue from a server that predates the field, as no delegate", () => {
    const parsed = IssueSchema.safeParse(serverIssue);
    expect(parsed.success).toBe(true);
    // null, not undefined: the screen renders "no delegate" without having to
    // distinguish "absent" from "unset".
    expect(parsed.success && parsed.data.delegate_type).toBeNull();
    expect(parsed.success && parsed.data.delegate_id).toBeNull();
  });

  it("parses an explicit null pair", () => {
    const parsed = IssueSchema.safeParse({
      ...serverIssue,
      delegate_type: null,
      delegate_id: null,
    });
    expect(parsed.success).toBe(true);
    expect(parsed.success && parsed.data.delegate_type).toBeNull();
  });

  // The fallback is what the detail screen shows when a response drifts; it
  // must be structurally complete for the delegate too, or the chip row reads
  // `undefined` off it.
  it("has no delegate on the drift fallback", () => {
    expect(EMPTY_ISSUE_FALLBACK.delegate_type ?? null).toBeNull();
    expect(EMPTY_ISSUE_FALLBACK.delegate_id ?? null).toBeNull();
  });
});
