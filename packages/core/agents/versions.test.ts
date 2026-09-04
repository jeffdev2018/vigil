// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { diffLines } from "./versions";

function stubFetchJson(body: unknown, status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
    ),
  );
}

afterEach(() => vi.unstubAllGlobals());

describe("diffLines", () => {
  it("keeps common lines and marks the rest", () => {
    expect(diffLines("a\nb\nc", "a\nx\nc\nd")).toEqual([
      { kind: "same", text: "a" },
      { kind: "removed", text: "b" },
      { kind: "added", text: "x" },
      { kind: "same", text: "c" },
      { kind: "added", text: "d" },
    ]);
    expect(diffLines("", "one")).toEqual([{ kind: "added", text: "one" }]);
    expect(diffLines("same", "same")).toEqual([{ kind: "same", text: "same" }]);
  });
});

describe("agent version endpoints", () => {
  it("lists defensively and rejects a shapeless diff or rollback", async () => {
    stubFetchJson({ versions: [{ id: "v2", version_number: 2, active: true, skill_ids: "nope", tool_config: [] }, { nope: 1 }] });
    expect(await new ApiClient("https://api.example.test").listAgentVersions("a")).toEqual([]);
    stubFetchJson({ versions: [{ id: "v2", version_number: 2, active: true, skill_ids: "nope", tool_config: [] }] });
    const [v] = await new ApiClient("https://api.example.test").listAgentVersions("a");
    expect(v).toMatchObject({ version_number: 2, skill_ids: [], tool_config: {}, model: "" });
    stubFetchJson({ changed_fields: ["model"] });
    await expect(new ApiClient("https://api.example.test").getAgentVersionDiff("a", "v2", "v1")).rejects.toThrow();
    stubFetchJson({ version: 3 });
    await expect(new ApiClient("https://api.example.test").rollbackAgentVersion("a", "v1")).rejects.toThrow();
  });
});
