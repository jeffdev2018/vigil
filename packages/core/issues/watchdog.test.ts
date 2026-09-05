// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { watchdogOutcome, type WatchdogVerdict } from "./watchdog";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

const verdict = (over: Partial<WatchdogVerdict> = {}): WatchdogVerdict => ({
  id: "v1", watchdog_id: "w1", issue_id: "i1", task_id: "t1", verdict: "motion", summary: "", findings: [], dropped: [], applied: {}, decision_id: null, human_review: "pending", contract_revision: 1, created_at: "", ...over,
});

describe("watchdog client and outcome", () => {
  it("parses the config, the verdict list and a review tolerantly", async () => {
    stubFetch({ watchdog: { id: "w1", agent_id: "a1", rest_minutes: "soon", enabled: 1 } });
    const w = await new ApiClient("https://api.example.test").getIssueWatchdog("i1");
    expect(w?.rest_minutes).toBe(30);
    expect(w?.enabled).toBe(true);
    stubFetch({ watchdog: null });
    expect(await new ApiClient("https://api.example.test").getIssueWatchdog("i1")).toBeNull();
    stubFetch({ verdicts: [{ id: "v1", verdict: "sideways", findings: [{ issue: "X-1", action: "reopen" }], human_review: "nope" }] });
    const list = await new ApiClient("https://api.example.test").listIssueWatchdogVerdicts("i1");
    expect(list[0]?.verdict).toBe("escalate");
    expect(list[0]?.human_review).toBe("pending");
    expect(list[0]?.findings[0]?.reason).toBe("");
    stubFetch("garbage");
    expect((await new ApiClient("https://api.example.test").reviewWatchdogVerdict("v1", false)).human_review).toBe("overturned");
  });

  it("names the outcome of a verdict from what was applied", () => {
    expect(watchdogOutcome(verdict({ applied: { escalated: true } }))).toBe("escalated");
    expect(watchdogOutcome(verdict({ applied: { reopened: 1, asked_proof: 0 } }))).toBe("reopened");
    expect(watchdogOutcome(verdict({ applied: { reopened: 0, asked_proof: 2 } }))).toBe("asked_proof");
    expect(watchdogOutcome(verdict({ applied: { dismissed: true }, human_review: "overturned" }))).toBe("dismissed");
    expect(watchdogOutcome(verdict({ verdict: "legitimate", applied: { noted: true } }))).toBe("noted");
    expect(watchdogOutcome(verdict({ decision_id: "d1" }))).toBe("awaiting_decision");
    expect(watchdogOutcome(verdict())).toBe("pending");
  });
});
