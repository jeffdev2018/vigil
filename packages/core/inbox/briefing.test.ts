// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { inboxKeys } from "./queries";

function stubFetchJson(body: unknown, status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
    ),
  );
}

afterEach(() => vi.unstubAllGlobals());

describe("morning briefing", () => {
  it("keeps the good sections when one is malformed and falls back on garbage", async () => {
    stubFetchJson({ date: "2026-09-04", merged: [{ issue_id: "a", identifier: "T-1", title: "Done" }], awaiting_review: "nope", blocked: [{ nope: 1 }], sent_at: 5 });
    const b = await new ApiClient("https://api.example.test").getMorningBriefingToday();
    expect(b.merged).toHaveLength(1);
    expect(b.awaiting_review).toEqual([]);
    expect(b.blocked).toEqual([]);
    expect(b.sent_at).toBeNull();
    stubFetchJson("garbage");
    expect(await new ApiClient("https://api.example.test").triggerMorningBriefing()).toEqual({ date: "", merged: [], awaiting_review: [], blocked: [], sent_at: null });
    expect(inboxKeys.briefing("w")).toEqual([...inboxKeys.all("w"), "briefing"]);
  });
});
