// @vitest-environment node
/**
 * Pins mobile delivery query keys and the shared wire schemas mobile uses
 * (imported from @multica/core/api/schemas — same as web).
 */
import { beforeAll, describe, expect, it, vi } from "vitest";

vi.mock("@/data/api", () => ({ api: {} }));
vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: () => null,
  getCurrentSlug: () => null,
}));

const id = "11111111-1111-4111-8111-111111111111";
const token = "a".repeat(64);
const snapshot = {
  title: "Empty inbox",
  description: null,
  criteria: ["Show a next step"],
  revision: 1,
  run: {
    id,
    status: "completed",
    result: { summary: "Fixed" },
    error: null,
    completed_at: "2026-09-05T00:00:00Z",
  },
  pull_requests: [],
};
const review = {
  id,
  decision: "accepted",
  feedback: "",
  assessments: [{ passed: true, evidence: "Checked at 390 px" }],
  snapshot,
  snapshot_token: token,
  reviewed_by: id,
  created_at: "2026-09-05T00:01:00Z",
};
const response = {
  ...snapshot,
  snapshot_token: token,
  latest_review: review,
  review_stale: false,
};

describe("mobile delivery query keys", () => {
  let deliveryKeys: typeof import("./queries/delivery").deliveryKeys;
  let issueDeliveryOptions: typeof import("./queries/delivery").issueDeliveryOptions;

  beforeAll(async () => {
    ({ deliveryKeys, issueDeliveryOptions } = await import(
      "./queries/delivery"
    ));
  });

  it("scopes delivery per workspace and issue", () => {
    expect(deliveryKeys.detail("a", id)).not.toEqual(
      deliveryKeys.detail("b", id),
    );
    expect(issueDeliveryOptions("a", id).queryKey).toEqual([
      "issueDelivery",
      "a",
      id,
    ]);
  });
});

describe("delivery wire schemas used by mobile", () => {
  it("parses delivery evidence with camelCase review fields", async () => {
    const { IssueDeliverySchema } = await import("@multica/core/api/schemas");
    const parsed = IssueDeliverySchema.safeParse(response);
    expect(parsed.success).toBe(true);
    if (parsed.success) {
      expect(parsed.data).toMatchObject({
        snapshotToken: token,
        latestReview: { reviewedBy: id, snapshotToken: token },
        run: { completedAt: "2026-09-05T00:00:00Z" },
        reviewStale: false,
      });
    }
  });

  it("fails closed on malformed evidence and unknown decisions", async () => {
    const { IssueDeliverySchema, DeliveryReviewSchema } = await import(
      "@multica/core/api/schemas"
    );
    expect(IssueDeliverySchema.safeParse({}).success).toBe(false);
    expect(
      IssueDeliverySchema.safeParse({
        ...response,
        latest_review: { ...review, decision: "future" },
      }).success,
    ).toBe(false);
    expect(
      DeliveryReviewSchema.safeParse({
        ...review,
        assessments: [{ passed: false, evidence: "Unchecked" }],
      }).success,
    ).toBe(false);
  });

  it("parses optional human_effort_seconds on reviews", async () => {
    const { DeliveryReviewSchema } = await import("@multica/core/api/schemas");
    const parsed = DeliveryReviewSchema.safeParse({
      ...review,
      human_effort_seconds: 18,
      review_delay_seconds: 60,
    });
    expect(parsed.success).toBe(true);
    if (parsed.success) {
      expect(parsed.data.humanEffortSeconds).toBe(18);
      expect(parsed.data.reviewDelaySeconds).toBe(60);
    }
  });
});
