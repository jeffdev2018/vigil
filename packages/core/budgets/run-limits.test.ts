// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { formatGateValue, runLimitKeys } from "./run-limits";

function stubFetch(body: unknown) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

describe("run limits client", () => {
  it("reads policies and events with tolerant fallbacks, and formats gate values", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ policies: [{ id: "p", scope_type: "team", max_turns: "x", max_cost_usd_ticks: 20000000000, action: "enforce" }] });
    const [p] = await client.listRunLimitPolicies();
    expect(p?.scope_type).toBe("workspace");
    expect(p?.max_turns).toBeNull();
    expect(p?.max_cost_usd_ticks).toBe(20000000000);
    expect(p?.warn_bps).toBe(8000);
    stubFetch("garbage");
    expect(await client.listRunLimitPolicies()).toEqual([]);
    stubFetch({ events: [{ task_id: "t", gate: "duration", level: "stopped", observed: 125, limit: 60 }] });
    const [e] = await client.listIssueRunLimitEvents("i");
    expect(e?.level).toBe("stopped");
    expect(formatGateValue("cost", 15000000000)).toBe("$1.50");
    expect(formatGateValue("duration", 125)).toBe("2m05s");
    expect(formatGateValue("duration", 3720)).toBe("1h02");
    expect(formatGateValue("turns", 7)).toBe("7");
    expect(runLimitKeys.issueEvents("w", "i")).toEqual(["run-limit-events", "w", "i"]);
  });
});
