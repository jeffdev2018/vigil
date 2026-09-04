// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { decisionKeys } from "./decisions";

function stubFetch(body: unknown) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

const record = { id: "d1", issue_id: "i1", run_id: "r1", source_message_seq: 4, title: "Keep it", context: "c", decision: "d" };

describe("decision memory client", () => {
  it("lists with the author filter and drops a malformed list", async () => {
    stubFetch({ decisions: [record] });
    const list = await new ApiClient("https://api.example.test").listProjectDecisions("p1", "agent");
    expect((globalThis.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0]?.[0]).toContain("/api/projects/p1/decisions?author_type=agent");
    expect(list[0]?.title).toBe("Keep it");
    expect(list[0]?.consequences).toBeNull();
    stubFetch({ decisions: [{ id: 4 }] });
    expect(await new ApiClient("https://api.example.test").listProjectDecisions("p1")).toEqual([]);
    stubFetch("nope");
    expect(await new ApiClient("https://api.example.test").listProjectDecisions("p1")).toEqual([]);
    expect(decisionKeys.project("w", "p", "")).toEqual(["decisions", "w", "p", ""]);
  });

  it("reads the ADR requirement and falls back to a satisfied gate", async () => {
    stubFetch({ required: true, satisfied: false, files: 12, file_threshold: 10, migration: true, decisions: 0 });
    const req = await new ApiClient("https://api.example.test").getIssueAdrRequirement("i1");
    expect(req.required).toBe(true);
    expect(req.files).toBe(12);
    stubFetch([]);
    expect((await new ApiClient("https://api.example.test").getIssueAdrRequirement("i1")).satisfied).toBe(true);
  });
});
