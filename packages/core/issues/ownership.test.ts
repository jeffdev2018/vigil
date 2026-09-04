// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";

function stubFetchJson(body: unknown, status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
    ),
  );
}

afterEach(() => vi.unstubAllGlobals());

describe("module ownership endpoints", () => {
  it("lists rules defensively and reads a suggestion or null", async () => {
    stubFetchJson({ rules: [{ id: "r1", owner_user_id: "u1", path_pattern: "packages/**" }, { nope: 1 }] });
    expect(await new ApiClient("https://api.example.test").listModuleOwnership()).toEqual([]);
    stubFetchJson({ rules: [{ id: "r1", owner_user_id: "u1", path_pattern: "packages/**" }] });
    const rules = await new ApiClient("https://api.example.test").listModuleOwnership();
    expect(rules[0]).toMatchObject({ id: "r1", label_id: null, referent_agent_id: null, priority: 0 });
    stubFetchJson({ suggestion: { rule_id: "r1", owner_user_id: "u1", matched: "path:a.ts", pattern: "**/*.ts" } });
    expect((await new ApiClient("https://api.example.test").getOwnershipSuggestion("i1"))?.owner_user_id).toBe("u1");
    stubFetchJson({ suggestion: { matched: 3 } });
    expect(await new ApiClient("https://api.example.test").getOwnershipSuggestion("i1")).toBeNull();
    stubFetchJson("garbage");
    expect(await new ApiClient("https://api.example.test").getOwnershipSuggestion("i1")).toBeNull();
  });
});
