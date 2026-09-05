// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";

function stubFetch(body: unknown, status = 200, headers: Record<string, string> = { "Content-Type": "application/json" }) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(typeof body === "string" ? body : JSON.stringify(body), { status, headers })));
}
afterEach(() => vi.unstubAllGlobals());

describe("workspace transfer client", () => {
  it("parses preview, report, runs and templates tolerantly", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ manifest: { format_version: 1, counts: { agents: 2 }, secrets: [{ scope: "agent", name: "a", key: "K" }] }, collisions: [{ kind: "agent", name: "a" }], strategies: ["skip", "bogus"] });
    const preview = await client.previewWorkspaceImport(new Blob(["zip"]));
    expect(preview.manifest.counts.agents).toBe(2);
    expect(preview.manifest.secrets[0]?.scoped).toBe(false);
    expect(preview.collisions[0]?.existing_id).toBe("");
    expect(preview.strategies).toEqual(["rename", "merge", "skip"]);
    stubFetch({ nope: 1 });
    expect((await client.previewWorkspaceImport(new Blob(["zip"]))).collisions).toEqual([]);
    stubFetch({ run_id: "r1", report: { created: { agents: 1 }, warnings: "nope" } });
    const result = await client.importWorkspace(new Blob(["zip"]), "merge", { a: { K: "v" } });
    expect(result.run_id).toBe("r1");
    expect(result.report.created.agents).toBe(1);
    expect(result.report.warnings).toEqual([]);
    stubFetch({ runs: [{ id: "x", direction: "sideways", status: "done" }] });
    const runs = await client.listWorkspaceTransferRuns();
    expect(runs[0]?.direction).toBe("export");
    expect(runs[0]?.status).toBe("failed");
    stubFetch({ templates: [{ id: "t", name: "T" }] });
    expect((await client.listWorkspaceTemplates())[0]?.report).toEqual({});
  });

  it("returns the export as a blob with its filename and run id", async () => {
    stubFetch("PK", 200, { "Content-Type": "application/zip", "Content-Disposition": 'attachment; filename="acme-20260101.multica.zip"', "X-Transfer-Run-ID": "run-1" });
    const out = await new ApiClient("https://api.example.test").exportWorkspace({ include_issues: true });
    expect(out.filename).toBe("acme-20260101.multica.zip");
    expect(out.runId).toBe("run-1");
    expect(out.blob.size).toBe(2);
    stubFetch("nope", 403);
    await expect(new ApiClient("https://api.example.test").exportWorkspace({})).rejects.toThrow("403");
  });
});
