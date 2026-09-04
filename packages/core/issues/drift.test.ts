// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { driftKeys } from "./drift";

function stubFetch(body: unknown) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

describe("drift policy client", () => {
  it("reads and writes the policy with tolerant fallbacks", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ enabled: "yes", repeated_action_threshold: 4, file_reread_threshold: "x" });
    const p = await client.getDriftPolicy();
    expect(p.enabled).toBe(true);
    expect(p.repeated_action_threshold).toBe(4);
    expect(p.file_reread_threshold).toBe(8);
    stubFetch({ enabled: false, repeated_action_threshold: 6, file_reread_threshold: 9 });
    expect((await client.putDriftPolicy({ enabled: false, repeated_action_threshold: 6, file_reread_threshold: 9 })).enabled).toBe(false);
    expect(driftKeys.policy("w")).toEqual(["drift-policy", "w"]);
  });
});
