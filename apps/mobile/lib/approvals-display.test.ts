// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { InboxItem } from "@multica/core/types";
import type { ApprovalGate, ApprovalItem } from "@/data/schemas";
import type { TimelineRow } from "@/lib/timeline-thread";
import {
  approvalIdOfRowId,
  approvalRowId,
  approvalSecondsLeft,
  formatCountdown,
  gateDetails,
  interleaveApprovals,
  matchApprovalForInboxItem,
} from "./approvals-display";

const approval = (over: Partial<ApprovalItem> = {}): ApprovalItem =>
  ({
    id: "app-1",
    source: "decision",
    kind: "decision",
    issue: { id: "issue-1", identifier: "MUL-1", title: "Ship it", status: "in_progress" },
    task_id: "",
    asked_by: { type: "agent", id: "agent-1", name: "Agent" },
    question: "Ship the migration tonight?",
    options: [],
    recommended_option_id: "",
    urgency: "normal",
    created_at: "2026-09-01T00:00:00Z",
    expires_at: null,
    sla_deadline_at: null,
    can_decide: true,
    cannot_decide_reason: "",
    gate: null,
    transition: null,
    goal_question: null,
    ...over,
  }) as ApprovalItem;

describe("approvalSecondsLeft / formatCountdown", () => {
  it("prefers expires_at over sla_deadline_at", () => {
    const now = new Date("2026-09-01T00:00:00Z");
    const seconds = approvalSecondsLeft(
      { expires_at: "2026-09-01T00:05:00Z", sla_deadline_at: "2026-09-01T01:00:00Z" },
      now,
    );
    expect(seconds).toBe(300);
  });

  it("falls back to sla_deadline_at when there is no gate expiry", () => {
    const now = new Date("2026-09-01T00:00:00Z");
    expect(
      approvalSecondsLeft({ expires_at: null, sla_deadline_at: "2026-09-01T00:10:00Z" }, now),
    ).toBe(600);
  });

  it("is null when neither deadline is set", () => {
    expect(approvalSecondsLeft({ expires_at: null, sla_deadline_at: null })).toBeNull();
  });

  it("clamps to 0 once the deadline has passed, never negative", () => {
    const now = new Date("2026-09-01T01:00:00Z");
    expect(
      approvalSecondsLeft({ expires_at: "2026-09-01T00:00:00Z", sla_deadline_at: null }, now),
    ).toBe(0);
  });

  it("formats minutes, hours and days like web's formatCountdown", () => {
    expect(formatCountdown(null)).toBe("");
    expect(formatCountdown(0)).toBe("0m");
    expect(formatCountdown(90)).toBe("1m");
    expect(formatCountdown(65 * 60)).toBe("1h 05m");
    expect(formatCountdown(50 * 3600)).toBe("2d");
  });
});

describe("gateDetails", () => {
  const gate = (details: Record<string, unknown>): ApprovalGate =>
    ({
      id: "gate-1",
      task_id: "task-1",
      gate_type: "git_push",
      summary: "",
      details,
      status: "pending",
      created_at: "",
      expires_at: null,
      resolved_at: null,
    }) as ApprovalGate;

  it("pulls paths, blast radius, params and approval progress out of the details bag", () => {
    const result = gateDetails(
      gate({
        paths: ["a.ts", "b.ts", 3],
        blast_radius: "prod",
        required_approvals: 2,
        approvers: ["u1", "u2"],
        params: { cmd: "rm -rf" },
      }),
    );
    expect(result.paths).toEqual(["a.ts", "b.ts"]);
    expect(result.blastRadius).toBe("prod");
    expect(result.requiredApprovals).toBe(2);
    expect(result.approvals).toBe(2);
    expect(result.params).toEqual({ cmd: "rm -rf" });
  });

  it("defaults requiredApprovals to 1 and tolerates a missing details bag", () => {
    expect(gateDetails(null).requiredApprovals).toBe(1);
    expect(gateDetails(undefined).paths).toEqual([]);
    expect(gateDetails(gate({})).requiredApprovals).toBe(1);
  });
});

describe("interleaveApprovals", () => {
  const row = (id: string, created_at: string): TimelineRow => ({
    entry: { id, type: "comment", actor_type: "member", actor_id: "u1", created_at },
    replies: [],
  });

  it("sorts pending asks into the timeline by time asked", () => {
    const rows = [row("c1", "2026-09-01T00:00:00Z"), row("c2", "2026-09-01T00:10:00Z")];
    const asks = [approval({ id: "a1", created_at: "2026-09-01T00:05:00Z" })];
    const merged = interleaveApprovals(rows, asks);
    expect(merged.map((r) => r.entry.id)).toEqual(["c1", approvalRowId("a1"), "c2"]);
  });

  it("returns the original rows unchanged when there are no pending asks", () => {
    const rows = [row("c1", "2026-09-01T00:00:00Z")];
    expect(interleaveApprovals(rows, [])).toBe(rows);
  });

  it("sorts an ask with an unparseable timestamp last", () => {
    const rows = [row("c1", "2026-09-01T00:00:00Z")];
    const asks = [approval({ id: "a1", created_at: "not-a-date" })];
    const merged = interleaveApprovals(rows, asks);
    expect(merged.map((r) => r.entry.id)).toEqual(["c1", approvalRowId("a1")]);
  });

  it("round-trips the sentinel row id", () => {
    expect(approvalIdOfRowId(approvalRowId("abc-123"))).toBe("abc-123");
    expect(approvalIdOfRowId("c1")).toBeNull();
  });
});

describe("matchApprovalForInboxItem", () => {
  const item = (over: Partial<InboxItem> = {}): Pick<InboxItem, "type" | "issue_id" | "details"> =>
    ({
      type: "decision_request",
      issue_id: "issue-1",
      details: null,
      ...over,
    }) as InboxItem;

  it("matches a decision_request by details.decision_id", () => {
    const target = approval({ id: "dec-1", source: "decision" });
    const other = approval({ id: "dec-2", source: "decision" });
    const found = matchApprovalForInboxItem(
      item({ details: { decision_id: "dec-1" } }),
      [other, target],
    );
    expect(found?.id).toBe("dec-1");
  });

  it("matches decision_escalated the same way as decision_request", () => {
    const target = approval({ id: "dec-1", source: "decision" });
    const found = matchApprovalForInboxItem(
      item({ type: "decision_escalated", details: { decision_id: "dec-1" } }),
      [target],
    );
    expect(found?.id).toBe("dec-1");
  });

  it("matches a transition_approval_requested by details.request_id", () => {
    const target = approval({ id: "req-1", source: "transition" });
    const found = matchApprovalForInboxItem(
      item({ type: "transition_approval_requested", details: { request_id: "req-1" } }),
      [target],
    );
    expect(found?.id).toBe("req-1");
  });

  it("matches a goal_question by details.goal_id", () => {
    const target = approval({ id: "goal-1", source: "goal_question" });
    const found = matchApprovalForInboxItem(
      item({ type: "goal_question", details: { goal_id: "goal-1" } }),
      [target],
    );
    expect(found?.id).toBe("goal-1");
  });

  it("falls back to issue_id + source when the detail id is missing or stale", () => {
    const target = approval({ id: "dec-9", source: "decision", issue: { id: "issue-1", identifier: "", title: "", status: "" } });
    const found = matchApprovalForInboxItem(
      item({ issue_id: "issue-1", details: { decision_id: "gone" } }),
      [target],
    );
    expect(found?.id).toBe("dec-9");
  });

  it("returns null once the ask has left the feed (already decided)", () => {
    expect(matchApprovalForInboxItem(item({ details: { decision_id: "dec-1" } }), [])).toBeNull();
  });

  it("returns null for an inbox item type with no approval counterpart", () => {
    expect(
      matchApprovalForInboxItem(item({ type: "issue_assigned" }), [approval()]),
    ).toBeNull();
  });
});
