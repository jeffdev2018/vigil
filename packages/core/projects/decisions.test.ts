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

  it("posts a hand-written decision and drops a malformed response", async () => {
    stubFetch({ decisions: [{ ...record, author_type: "member" }] });
    const client = new ApiClient("https://api.example.test");
    const created = await client.createIssueDecisions("i1", {
      run_id: "r1",
      decisions: [{ source_message_seq: 4, title: "Keep it", decision: "d", context: "c" }],
    });
    const call = (globalThis.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0];
    expect(call?.[0]).toContain("/api/issues/i1/decision-records");
    const init = call?.[1] as { method?: string; body?: string };
    expect(init?.method).toBe("POST");
    // The endpoint reads `decisions` as an array and `run_id` at the top
    // level; a body shaped any other way is a 400.
    expect(JSON.parse(init?.body ?? "{}")).toEqual({
      run_id: "r1",
      decisions: [{ source_message_seq: 4, title: "Keep it", decision: "d", context: "c" }],
    });
    expect(created[0]?.author_type).toBe("member");

    // Drift must not crash the dialog's success path.
    stubFetch({ decisions: [{ id: 7 }] });
    expect(await new ApiClient("https://api.example.test").createIssueDecisions("i1", { decisions: [] })).toEqual([]);
    stubFetch("nope");
    expect(await new ApiClient("https://api.example.test").createIssueDecisions("i1", { decisions: [] })).toEqual([]);
  });

  it("omits run_id when the caller has none, letting the server default to the last completed run", async () => {
    stubFetch({ decisions: [record] });
    await new ApiClient("https://api.example.test").createIssueDecisions("i1", {
      decisions: [{ source_message_seq: 2, title: "t", decision: "d" }],
    });
    const init = (globalThis.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0]?.[1] as { body?: string };
    expect(JSON.parse(init?.body ?? "{}")).not.toHaveProperty("run_id");
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
