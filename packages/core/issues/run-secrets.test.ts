// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { groupRunSecrets, runSecretKeys, type RunSecret } from "./run-secrets";

function stubFetch(body: unknown) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

describe("run secrets client", () => {
  it("reads keys and statuses, never a value, and tolerates garbage", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ secrets: [{ id: "s1", task_id: "t1", key: "API_KEY", status: "active", expires_at: "x", value: "leak" }, { id: "s2", task_id: "t1", key: "B", status: "weird" }] });
    const list = await client.listIssueRunSecrets("i1");
    expect(list.map((s) => [s.key, s.status])).toEqual([["API_KEY", "active"], ["B", "revoked"]]);
    expect(JSON.stringify(list)).not.toContain("leak");
    stubFetch("garbage");
    expect(await client.listIssueRunSecrets("i1")).toEqual([]);
    const s = (over: Partial<RunSecret>): RunSecret => ({ id: "", task_id: "t", key: "K", status: "active", expires_at: "", revoked_at: null, revoke_reason: null, created_at: "", ...over });
    expect(groupRunSecrets([s({ task_id: "t2", key: "A" }), s({ task_id: "t1" }), s({ task_id: "t2", key: "B" })]).map((g) => [g.taskId, g.secrets.length])).toEqual([["t2", 2], ["t1", 1]]);
    expect(runSecretKeys.issue("w", "i")).toEqual(["issues", "w", "run-secrets", "i"]);
  });
});
