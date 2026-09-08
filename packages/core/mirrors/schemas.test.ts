// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { isMirrorOpen } from "./schemas";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

// K54: mirror links and mirrors must survive a backend that drifts, because an
// installed desktop build keeps talking to a newer server.
describe("mirror client", () => {
  it("parses mirror links tolerantly", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ links: [{ id: "l1", target_project_id: "p2", target_project_title: 42, trigger_label: "mirror" }] });
    const list = await client.listMirrorLinks("p1");
    expect(list.links[0]?.target_project_id).toBe("p2");
    // A wrong-typed title falls back rather than throwing the whole list away.
    expect(list.links[0]?.target_project_title).toBe("");

    // A response with no `links` at all still yields an array to render.
    stubFetch({ nope: 1 });
    expect((await client.listMirrorLinks("p1")).links).toEqual([]);
  });

  it("parses issue mirrors and the mirror_of banner tolerantly", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({
      mirrors: [{ id: "m1", mirror_issue_id: "i2", identifier: "JIA-9", number: "nine", status: "todo", type_synced: "yes" }],
      mirror_of: { id: "m0", source_issue_id: "i1", identifier: "JIA-1", status: "in_progress" },
    });
    const got = await client.getIssueMirrors("i1");
    expect(got.mirrors[0]?.identifier).toBe("JIA-9");
    expect(got.mirrors[0]?.number).toBe(0);
    expect(got.mirrors[0]?.type_synced).toBe(false);
    expect(got.mirror_of?.source_issue_id).toBe("i1");

    // No banner is the ordinary case and must read as null, never undefined.
    stubFetch({ mirrors: [] });
    expect((await client.getIssueMirrors("i1")).mirror_of).toBeNull();

    stubFetch("not json at all");
    expect((await client.getIssueMirrors("i1")).mirrors).toEqual([]);
  });

  it("echoes the requested value when the toggle response is malformed", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ garbage: true });
    expect(await client.setMirrorTypeSynced("i1", "m1", true)).toEqual({ id: "m1", type_synced: true });
  });
});

// A mirror stops holding its source back once it reaches a terminal status.
describe("isMirrorOpen", () => {
  it.each([
    ["todo", true],
    ["in_progress", true],
    ["in_review", true],
    ["done", false],
    ["cancelled", false],
  ])("%s -> %s", (status, open) => {
    expect(isMirrorOpen({ status })).toBe(open);
  });
});
