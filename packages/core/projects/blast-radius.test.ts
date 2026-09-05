// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { blastRadiusKeys, blastRadiusPreviewOptions } from "./blast-radius";

function stubFetch(body: unknown) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

describe("blast radius client", () => {
  it("lists rules, previews a path and tolerates malformed answers", async () => {
    stubFetch({ rules: [{ id: "r1", path_pattern: "apps/mobile/**", autonomy_level: "autonomous", specificity: 12 }], levels: ["autonomous", "read_only", "dual_approval"] });
    const list = await new ApiClient("https://api.example.test").listBlastRadiusRules("p1");
    expect(list.rules[0]?.specificity).toBe(12);
    stubFetch({ path: "server/migrations/1.sql", level: "read_only", rule_id: "r2", path_pattern: "server/migrations/**" });
    expect((await new ApiClient("https://api.example.test").previewBlastRadius("p1", "server/migrations/1.sql")).level).toBe("read_only");
    stubFetch("nope");
    expect((await new ApiClient("https://api.example.test").previewBlastRadius("p1", "x")).level).toBe("inherit");
    stubFetch({ rule: { nope: 1 } });
    await expect(new ApiClient("https://api.example.test").createBlastRadiusRule("p1", { path_pattern: "x", autonomy_level: "read_only" })).rejects.toThrow();
    expect(blastRadiusPreviewOptions("w", "p", "  ").enabled).toBe(false);
    expect(blastRadiusKeys.rules("w", "p")).toEqual(["blast-radius-rules", "w", "p"]);
  });
});
