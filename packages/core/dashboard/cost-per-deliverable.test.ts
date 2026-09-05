// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";

function stubFetchJson(body: unknown, status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
    ),
  );
}

afterEach(() => vi.unstubAllGlobals());

describe("getDashboardCostPerDeliverable", () => {
  it("passes the filters and keeps a malformed section at zero", async () => {
    stubFetchJson({ days: 7, issues: { count: 2, mean_usd_ticks: 5, median_usd_ticks: 4, total_usd_ticks: 10, uncosted_count: 0, trend_pct: "x" }, pull_requests: "nope" });
    const out = await new ApiClient("https://api.example.test").getDashboardCostPerDeliverable({ days: 7, project_id: "p1", tz: "Asia/Tokyo" });
    expect((globalThis.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0]?.[0]).toContain("cost-per-deliverable?days=7&project_id=p1&tz=Asia%2FTokyo");
    expect(out.issues.count).toBe(2);
    expect(out.issues.trend_pct).toBeNull();
    expect(out.pull_requests.count).toBe(0);
  });

  it("falls back to an empty response on garbage", async () => {
    stubFetchJson([1, 2]);
    const out = await new ApiClient("https://api.example.test").getDashboardCostPerDeliverable({ days: 30 });
    expect(out).toEqual({ days: 30, issues: expect.objectContaining({ count: 0 }), pull_requests: expect.objectContaining({ count: 0 }) });
  });
});
