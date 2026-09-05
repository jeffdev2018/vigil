// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { auditKeys } from "./audit";

function stubFetch(body: string, status = 200, type = "application/json") {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(body, { status, headers: { "Content-Type": type } })));
}

afterEach(() => vi.unstubAllGlobals());

describe("audit log client", () => {
  it("passes filters and cursor, and keeps a malformed page usable", async () => {
    stubFetch(JSON.stringify({ entries: [{ id: "e1", action: "issue.status_changed", details: "nope" }, { nope: 1 }], next_cursor: 7 }));
    const page = await new ApiClient("https://api.example.test").listAuditLog({ actor_type: "member", action: "x" }, "c1", 10);
    expect((globalThis.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0]?.[0]).toContain("actor_type=member&action=x&cursor=c1&limit=10");
    expect(page.entries).toEqual([]);
    expect(page.next_cursor).toBe("");
    stubFetch(JSON.stringify({ entries: [{ id: "e1", action: "issue.status_changed", details: "nope" }], next_cursor: "n" }));
    const ok = await new ApiClient("https://api.example.test").listAuditLog({});
    expect(ok.entries[0]?.details).toEqual({});
    expect(ok.next_cursor).toBe("n");
    expect(auditKeys.list("w", { action: "a" })).toEqual(["audit-log", "w", "", "", "", "a"]);
  });

  it("reads the chain status and falls back to not ok on garbage", async () => {
    stubFetch(JSON.stringify({ ok: true, total: 12, head_hash: "abc", broken_seq: null, broken_id: null }));
    expect((await new ApiClient("https://api.example.test").verifyAuditLog()).head_hash).toBe("abc");
    stubFetch("[]");
    expect((await new ApiClient("https://api.example.test").verifyAuditLog()).ok).toBe(false);
  });

  it("returns the export text and throws on a failed export", async () => {
    stubFetch("id,action\n1,x\n", 200, "text/csv");
    expect(await new ApiClient("https://api.example.test").exportAuditLog("csv", {})).toContain("id,action");
    stubFetch("nope", 403);
    await expect(new ApiClient("https://api.example.test").exportAuditLog("json", {})).rejects.toThrow();
  });
});
