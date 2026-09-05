// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { contestCostUsd, contestIsLive, pairContestRows } from "./contest";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

describe("contest client and helpers", () => {
  it("parses contests, a preflight and settings tolerantly", async () => {
    stubFetch({ contests: [{ id: "c1", status: "weird", objections: [{ n: 1, severity: "HUGE", claim: "x" }], answers: "nope" }] });
    const list = await new ApiClient("https://api.example.test").listContests({ issue_id: "i1" });
    expect(list[0]?.status).toBe("running");
    expect(list[0]?.objections[0]?.severity).toBe("medium");
    expect(list[0]?.answers).toEqual([]);
    stubFetch("garbage");
    expect(await new ApiClient("https://api.example.test").listContests({ issue_id: "i1" })).toEqual([]);
    stubFetch({ challenger: { kind: "llm", name: "service model" }, quota_limit: "10" });
    const pf = await new ApiClient("https://api.example.test").preflightContest({ target_type: "plan", target_id: "p1" });
    expect(pf?.challenger.kind).toBe("llm");
    expect(pf?.quota_limit).toBe(0);
    stubFetch({ targets: { plan: true }, opt_out_project_ids: null });
    const settings = await new ApiClient("https://api.example.test").getContestSettings();
    expect(settings.targets.plan).toBe(true);
    expect(settings.opt_out_project_ids).toEqual([]);
  });

  it("pairs objections with answers, knows liveness and formats cost", () => {
    const rows = pairContestRows({
      objections: [{ n: 1, severity: "high", kind: "missing", claim: "a", evidence: "", expected_proof: "" }, { n: 2, severity: "low", kind: "risky", claim: "b", evidence: "", expected_proof: "" }],
      answers: [{ n: 2, verdict: "refute", note: "no", proof: "doc" }],
    });
    expect(rows[0]?.answer).toBeNull();
    expect(rows[1]?.answer?.verdict).toBe("refute");
    expect(contestIsLive({ status: "answering" })).toBe(true);
    expect(contestIsLive({ status: "answered" })).toBe(false);
    expect(contestCostUsd(1_234_567)).toBe("1.23");
  });
});
