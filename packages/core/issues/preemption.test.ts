// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { preemptionKeys } from "./preemption";

function stubFetch(body: unknown) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

describe("preemption client", () => {
  it("reads preemptions with tolerant fallbacks", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ preemptions: [{ task_id: "t", status: "paused", preempted_by_task_id: "u", preempted_by_identifier: "JEF-9", resumed_by_task_id: 5 }] });
    const [p] = await client.listIssuePreemptions("i1");
    expect(p?.preempted_by_identifier).toBe("JEF-9");
    expect(p?.resumed_by_task_id).toBeNull();
    stubFetch("garbage");
    expect(await client.listIssuePreemptions("i1")).toEqual([]);
    expect(preemptionKeys.issue("w", "i")).toEqual(["preemptions", "w", "i"]);
  });
});
