// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { crossReviewKeys, crossReviewState, type CrossReview } from "./cross-review";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

const review = (status: string): CrossReview => ({ task_id: "t", review_of_task_id: "a", reviewer_agent_id: "r", reviewer_name: "Rev", reviewer_provider: "codex", status, report: null, created_at: "", completed_at: null });

describe("cross-review client", () => {
  it("parses reviews with fallbacks and derives the state", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ reviews: [{ task_id: "t1", status: "completed", report: { verdict: "odd", risks: "no", questions: ["q"] } }, { task_id: "t2", status: "failed", report: "garbage" }] });
    const rs = await client.listCrossReviews("i1");
    expect(rs[0]?.report?.verdict).toBe("comment");
    expect(rs[0]?.report?.risks).toEqual([]);
    expect(rs[0]?.report?.questions).toEqual(["q"]);
    expect(rs[1]?.report).toBeNull();
    stubFetch("garbage");
    expect(await client.listCrossReviews("i1")).toEqual([]);
    stubFetch({ reviews: [{ task_id: "t3", status: "queued" }] }, 201);
    expect((await client.retryCrossReview("i1"))[0]?.task_id).toBe("t3");
    expect(crossReviewState(review("queued"))).toBe("in_progress");
    expect(crossReviewState(review("paused"))).toBe("in_progress");
    expect(crossReviewState(review("failed"))).toBe("failed");
    expect(crossReviewState(review("cancelled"))).toBe("failed");
    expect(crossReviewState(review("completed"))).toBe("done");
    expect(crossReviewKeys.issue("w", "i")).toEqual(["cross-reviews", "w", "i"]);
  });
});
