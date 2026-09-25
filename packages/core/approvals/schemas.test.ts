// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import {
  ApprovalsResponseSchema,
  EMPTY_APPROVALS,
  approvalSecondsLeft,
  approvalsAskedBy,
  formatCountdown,
  gateDetails,
} from "./schemas";

// The canonical place the feed shape is proven; the ApprovalCard suite keeps
// the happy path and the wiring.

const gateItem = {
  id: "d1",
  source: "decision",
  kind: "gate",
  issue: { id: "i1", identifier: "ONE-1", title: "Ship", status: "in_progress" },
  task_id: "t1",
  asked_by: { type: "agent", id: "a1", name: "Bot" },
  question: "Blocked action · delete_customer",
  options: [{ id: "approve", label: "Approve" }, { id: "deny", label: "Deny", impact: "refused" }],
  urgency: "high",
  created_at: "2026-09-09T10:00:00Z",
  expires_at: "2026-09-09T10:30:00Z",
  sla_deadline_at: null,
  can_decide: true,
  gate: { id: "g1", task_id: "t1", gate_type: "mcp_tool_call", summary: "delete_customer", details: { params: { id: "c-1" }, paths: ["crm/customers"], required_approvals: 2, approvers: ["u1"] }, status: "pending", created_at: "", expires_at: "2026-09-09T10:30:00Z", resolved_at: null },
};

describe("ApprovalsResponseSchema", () => {
  it("parses a feed and keeps unknown kinds from a newer server", () => {
    const parsed = ApprovalsResponseSchema.parse({ approvals: [gateItem, { ...gateItem, id: "d2", kind: "brand_new" }], total: 2, run_halt: { halted: true, reason: "incident" } });
    expect(parsed.approvals).toHaveLength(2);
    expect(parsed.approvals[0]?.gate?.gate_type).toBe("mcp_tool_call");
    expect(parsed.approvals[1]?.kind).toBe("brand_new");
    expect(parsed.run_halt.halted).toBe(true);
  });
  it("falls back to the empty feed on garbage", () => {
    expect(parseWithFallback("nope", ApprovalsResponseSchema, EMPTY_APPROVALS, { endpoint: "test" })).toEqual(EMPTY_APPROVALS);
    const parsed = ApprovalsResponseSchema.parse({ approvals: "x", total: "y" });
    expect(parsed.approvals).toEqual([]);
    expect(parsed.run_halt.halted).toBe(false);
  });
});

describe("helpers", () => {
  it("counts down to the gate's expiry before the SLA", () => {
    const now = new Date("2026-09-09T10:10:00Z");
    expect(approvalSecondsLeft(gateItem, now)).toBe(20 * 60);
    expect(approvalSecondsLeft({ expires_at: null, sla_deadline_at: "2026-09-09T12:10:00Z" }, now)).toBe(2 * 3600);
    expect(approvalSecondsLeft({ expires_at: null, sla_deadline_at: null }, now)).toBeNull();
    expect(approvalSecondsLeft({ expires_at: "2026-09-09T09:00:00Z", sla_deadline_at: null }, now)).toBe(0);
  });
  it("formats a countdown compactly", () => {
    expect(formatCountdown(null)).toBe("");
    expect(formatCountdown(59)).toBe("0m");
    expect(formatCountdown(20 * 60)).toBe("20m");
    expect(formatCountdown(3 * 3600 + 5 * 60)).toBe("3h 05m");
    expect(formatCountdown(49 * 3600)).toBe("2d");
  });
  it("reads gate details defensively", () => {
    const d = gateDetails(ApprovalsResponseSchema.parse({ approvals: [gateItem] }).approvals[0]?.gate);
    expect(d.paths).toEqual(["crm/customers"]);
    expect(d.requiredApprovals).toBe(2);
    expect(d.approvals).toBe(1);
    expect(d.params).toEqual({ id: "c-1" });
    expect(gateDetails(null)).toEqual({ params: null, paths: [], blastRadius: "", requiredApprovals: 1, approvals: 0 });
  });
  it("filters the asks of one agent", () => {
    const items = ApprovalsResponseSchema.parse({ approvals: [gateItem, { ...gateItem, id: "d2", asked_by: { type: "member", id: "u1" } }] }).approvals;
    expect(approvalsAskedBy(items as never, "a1").map((a) => a.id)).toEqual(["d1"]);
    expect(approvalsAskedBy(items as never, "")).toEqual([]);
  });
});
