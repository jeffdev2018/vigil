// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { inboxKeys } from "./queries";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

describe("inbox decisions client (K63)", () => {
  it("parses the capped list with its total and tolerates drift", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ decisions: [{ inbox_item_id: "ib", issue_id: "i", issue_identifier: "ACME-1", risk_score: "x", decision: { id: "d", question: "Q?", options: "bad", urgency: "high" } }], total: 7 });
    const out = await client.listInboxDecisions();
    expect(out.total).toBe(7);
    expect(out.decisions[0]?.risk_score).toBe(0);
    expect(out.decisions[0]?.decision.options).toEqual([]);
    stubFetch("garbage");
    expect(await client.listInboxDecisions()).toEqual({ decisions: [], total: 0 });
    expect(inboxKeys.decisions("w")).toEqual(["inbox", "w", "decisions"]);
  });
});
