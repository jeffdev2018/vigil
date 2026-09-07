// @vitest-environment node
/**
 * Pins mobile decision query keys and the shared wire schemas mobile uses
 * (imported from @multica/core/api/schemas — same as web).
 */
import { beforeAll, describe, expect, it, vi } from "vitest";

vi.mock("@/data/api", () => ({ api: {} }));
vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: () => null,
  getCurrentSlug: () => null,
}));

const id = "11111111-1111-4111-8111-111111111111";
const row = {
  id,
  issue_id: id,
  agent_id: id,
  source_task_id: id,
  recipient_id: id,
  requested_by: id,
  requester_type: "agent",
  question: "A or B?",
  context: "Tradeoffs",
  options: ["A", "B"],
  status: "open",
  answer: null,
  answered_by: null,
  answered_at: null,
  resume_task_id: null,
  created_at: "2026-09-06T00:00:00Z",
};

describe("mobile decision query keys", () => {
  let decisionKeys: typeof import("./queries/decisions").decisionKeys;
  let inboxDecisionsOptions: typeof import("./queries/decisions").inboxDecisionsOptions;

  beforeAll(async () => {
    ({ decisionKeys, inboxDecisionsOptions } = await import(
      "./queries/decisions"
    ));
  });

  it("scopes pending and history separately per workspace", () => {
    expect(decisionKeys.list("a", false)).not.toEqual(decisionKeys.list("b", false));
    expect(inboxDecisionsOptions("a").queryKey).not.toEqual(
      inboxDecisionsOptions("a", true).queryKey,
    );
    expect(inboxDecisionsOptions("a").queryKey).toEqual([
      "inboxDecisions",
      "a",
      false,
    ]);
  });
});

describe("decision wire schemas used by mobile", () => {
  it("parses a durable open decision page", async () => {
    const { IssueDecisionPageSchema } = await import(
      "@multica/core/api/schemas"
    );
    const parsed = IssueDecisionPageSchema.safeParse({
      decisions: [row],
      next_before_id: id,
    });
    expect(parsed.success).toBe(true);
    if (parsed.success) {
      expect(parsed.data).toMatchObject({
        decisions: [{ issueId: id, sourceTaskId: id, status: "open" }],
        nextBeforeId: id,
      });
    }
  });

  it("rejects malformed or future statuses fail-closed", async () => {
    const { IssueDecisionSchema, IssueDecisionPageSchema } = await import(
      "@multica/core/api/schemas"
    );
    expect(IssueDecisionSchema.safeParse({}).success).toBe(false);
    expect(
      IssueDecisionSchema.safeParse({ ...row, status: "future" }).success,
    ).toBe(false);
    expect(
      IssueDecisionSchema.safeParse({ ...row, answer: "A" }).success,
    ).toBe(false);
    expect(
      IssueDecisionPageSchema.safeParse({
        decisions: [],
        next_before_id: 4,
      }).success,
    ).toBe(false);
  });
});
