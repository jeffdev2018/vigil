// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { ciAutoFixKeys, ciAutoFixState, type CIAutoFixRun } from "./ci-auto-fix";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

const run = (pr: string, task_status: string, attempt: number): CIAutoFixRun => ({ id: "r" + attempt, provider: "vcs", pull_request_id: pr, head_sha: "sha", issue_id: "i", task_id: "t", task_status, attempt, budget_usd_ticks: 0, manual: false, created_at: "" });

describe("ci auto-fix client (K49)", () => {
  it("parses with fallbacks and derives the chip state", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ runs: [{ id: "r", pull_request_id: "p", task_status: "running", attempt: "x" }], enabled: true, max_attempts: "3" });
    const out = await client.getIssueCIAutoFix("i1");
    expect(out.enabled).toBe(true);
    expect(out.max_attempts).toBe(3);
    expect(out.runs[0]?.attempt).toBe(0);
    stubFetch("garbage");
    expect(await client.getIssueCIAutoFix("i1")).toEqual({ runs: [], enabled: false, max_attempts: 3 });
    stubFetch({ run: { id: "r9", pull_request_id: "p", task_status: "queued", attempt: 3, manual: true } }, 201);
    expect((await client.retryCIAutoFix("p"))?.manual).toBe(true);
    expect(ciAutoFixState([], "p", 3)).toEqual({ state: "none", attempts: 0 });
    expect(ciAutoFixState([run("p", "running", 1)], "p", 3).state).toBe("in_progress");
    expect(ciAutoFixState([run("p", "completed", 1)], "p", 3).state).toBe("fixed");
    expect(ciAutoFixState([run("p", "failed", 2), run("p", "completed", 1)], "p", 3).state).toBe("failed");
    expect(ciAutoFixState([run("p", "failed", 3), run("p", "failed", 2), run("p", "failed", 1)], "p", 3)).toEqual({ state: "exhausted", attempts: 3 });
    expect(ciAutoFixState([run("other", "failed", 1)], "p", 3).state).toBe("none");
    expect(ciAutoFixKeys.issue("w", "i")).toEqual(["ci-auto-fix", "w", "i"]);
  });
});
