// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { draftOrigin, type SkillDraft } from "./drafts";

function stubFetch(body: unknown) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

describe("skill drafts", () => {
  it("parses the draft list tolerantly", async () => {
    stubFetch({ drafts: [{ id: "s1", name: "mined-tests", sources: [{ issue_number: "12" }, { issue_id: "i2", issue_number: 7, status_regressed: true }] }] });
    const drafts = await new ApiClient("https://api.example.test").listSkillDrafts();
    expect(drafts[0]?.status).toBe("draft");
    expect(drafts[0]?.sources[0]?.issue_number).toBe(0);
    expect(drafts[0]?.sources[1]?.status_regressed).toBe(true);
    stubFetch("garbage");
    expect(await new ApiClient("https://api.example.test").listSkillDrafts()).toEqual([]);
  });

  it("reads where a draft came from", () => {
    const d: SkillDraft = { id: "s1", workspace_id: "w", name: "n", description: "", config: { origin: { type: "skill_miner", agent_name: "Builder", signals: 3, status_regressed: 1, llm: false } }, created_by: null, created_at: "", updated_at: "", status: "draft", sources: [] };
    expect(draftOrigin(d)).toEqual({ type: "skill_miner", agent_name: "Builder", signals: 3, regressed: 1, llm: false });
    expect(draftOrigin({ ...d, config: {} }).type).toBe("manual");
  });
});
