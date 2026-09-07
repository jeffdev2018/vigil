// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { deliveryKeys, issueDeliveryOptions } from "../issues/delivery";

const api = new ApiClient("https://api.example.test");
const id = "11111111-1111-4111-8111-111111111111";
const token = "a".repeat(64);
const snapshot = {
  title: "Empty inbox", description: null, criteria: ["Show a next step"], revision: 1,
  run: { id, status: "completed", result: { summary: "Fixed" }, error: null, completed_at: "2026-09-05T00:00:00Z" },
  pull_requests: [],
};
const review = { id, decision: "accepted", feedback: "", assessments: [{ passed: true, evidence: "Checked at 390 px" }],
  snapshot, snapshot_token: token, reviewed_by: id, created_at: "2026-09-05T00:01:00Z" };
const response = { ...snapshot, snapshot_token: token, latest_review: review, review_stale: false };
const input = { reviewId: id, expectedReviewId: "", snapshotToken: token, decision: "accepted" as const,
  feedback: "", assessments: review.assessments };

function stub(body: unknown) {
  const fetch = vi.fn().mockImplementation(async () => new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } }));
  vi.stubGlobal("fetch", fetch);
  return fetch;
}
afterEach(() => vi.unstubAllGlobals());

describe("delivery API contract", () => {
  it("preserves evidence identity and translates request fields", async () => {
    stub(response);
    expect(await api.getIssueDelivery(id)).toMatchObject({ snapshotToken: token, latestReview: { reviewedBy: id, snapshotToken: token },
      run: { completedAt: "2026-09-05T00:00:00Z" }, pullRequests: [] });
    const fetch = stub(review);
    expect(await api.reviewIssueDelivery(id, input)).toMatchObject({ id, decision: "accepted" });
    expect(JSON.parse(fetch.mock.calls[0]![1].body)).toEqual({ review_id: id, expected_review_id: "", snapshot_token: token,
      decision: "accepted", feedback: "", assessments: review.assessments });
    stub({ criteria: ["Updated"], revision: 2 });
    expect(await api.updateIssueDeliveryCriteria(id, ["Updated"], 1)).toEqual({ criteria: ["Updated"], revision: 2 });
  });

  it("fails closed on malformed evidence, approvals, and unknown decisions", async () => {
    for (const body of [null, {}, { ...response, snapshot_token: "" }, { ...response, latest_review: { ...review, decision: "future" } },
      { ...response, latest_review: { ...review, assessments: [{ passed: "true", evidence: "Checked" }] } },
      { ...response, latest_review: { ...review, assessments: [] } }]) {
      stub(body);
      expect(await api.getIssueDelivery(id)).toBeNull();
    }
    for (const body of [null, {}, { ...review, assessments: [{ passed: false, evidence: "Unchecked" }] }, { ...review, reviewed_by: null }]) {
      stub(body);
      expect(await api.reviewIssueDelivery(id, input)).toBeNull();
    }
    stub({ criteria: [], revision: -1 });
    expect(await api.updateIssueDeliveryCriteria(id, [], 1)).toBeNull();
  });

  it("detects outdated acceptance even if the backend freshness boolean drifts", async () => {
    stub({ ...response, snapshot_token: "b".repeat(64) });
    expect((await api.getIssueDelivery(id))?.reviewStale).toBe(true);
    expect(issueDeliveryOptions("ws-a", id).queryKey).toEqual(deliveryKeys.detail("ws-a", id));
    expect(issueDeliveryOptions("ws-a", id).queryKey).not.toEqual(issueDeliveryOptions("ws-b", id).queryKey);
  });

  it("requires a correction receipt for this review and preserves the persisted run link", async () => {
    const fetch = stub({ review_id: id, task_id: id });
    expect(await api.startIssueDeliveryCorrection(id, id)).toEqual({ reviewId: id, taskId: id });
    expect(fetch.mock.calls[0]![0]).toBe(`https://api.example.test/api/issues/${id}/delivery/correction`);
    expect(JSON.parse(fetch.mock.calls[0]![1].body)).toEqual({ review_id: id });
    for (const body of [null, {}, { review_id: id, task_id: "" }, { review_id: "22222222-2222-4222-8222-222222222222", task_id: id }]) {
      stub(body);
      expect(await api.startIssueDeliveryCorrection(id, id)).toBeNull();
    }
    stub({ ...response, latest_review: { ...review, decision: "changes_requested", correction_task_id: id } });
    expect((await api.getIssueDelivery(id))?.latestReview?.correctionTaskId).toBe(id);
    stub({ ...response, latest_review: { ...review, correction_task_id: true } });
    expect(await api.getIssueDelivery(id)).toBeNull();
  });
});

it("preserves frozen costs, treats malformed amounts as unavailable, and validates history", async () => {
  const usage = { captured_at: "2026-09-05T00:01:00Z", status: "reported", available_usd: "1844674407.3709551614", reported_usd: "1844674407.3709551614", estimated_usd: "0.0000000000", run_ids: [id], runs_without_usage: 0, nonterminal_runs: 0, unpriced_slices: 0 };
  stub({ ...response, latest_review: { ...review, usage_snapshot: usage, review_delay_seconds: 60 }, metrics: { review_count: 2, reviewed_results: 1, accepted_results: 1, correction_requests: 0, acceptance_reversals: 0 } });
  const parsed = await api.getIssueDelivery(id);
  expect(parsed?.latestReview?.usageSnapshot?.availableUsd).toBe("1844674407.3709551614");
  expect(parsed?.latestReview?.reviewDelaySeconds).toBe(60);
  expect(parsed?.latestReview?.humanEffortSeconds).toBeNull();
  stub({ ...response, latest_review: { ...review, usage_snapshot: usage, review_delay_seconds: 60, human_effort_seconds: 18 }, metrics: { review_count: 2, reviewed_results: 1, accepted_results: 1, correction_requests: 0, acceptance_reversals: 0 } });
  expect((await api.getIssueDelivery(id))?.latestReview?.humanEffortSeconds).toBe(18);
  expect(parsed?.metrics?.acceptedResults).toBe(1);
  for (const extra of [{ available_usd: -1 }, { available_usd: "NaN" }, { available_usd: "0.0000000000" }, { runs_without_usage: 1 }, { status: "future" }]) {
    stub({ ...response, latest_review: { ...review, usage_snapshot: { ...usage, ...extra } } });
    expect((await api.getIssueDelivery(id))?.latestReview?.usageSnapshot).toBeNull();
  }
  const fetch = stub({ reviews: [ { ...review, usage_snapshot: usage } ], next_before_id: id });
  expect((await api.getIssueDeliveryHistory(id, id))?.reviews[0]?.usageSnapshot?.availableUsd).toBe(usage.available_usd);
  expect(fetch.mock.calls[0]?.[0]).toBe(`https://api.example.test/api/issues/${id}/delivery/reviews?before_id=${id}`);
  stub({ reviews: [ { ...review, snapshot: null } ], next_before_id: null });
  expect(await api.getIssueDeliveryHistory(id)).toBeNull();
});
