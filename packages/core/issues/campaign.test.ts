// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { campaignKeys, campaignProgress, campaignShardSkippable, type CampaignShard, type RefactorCampaign } from "./campaign";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

const shard = (over: Partial<CampaignShard>): CampaignShard => ({ id: "s", child_issue_id: "c", task_id: "t", task_status: "completed", run_outcome: "completed", assignee_agent_id: "a", description: "", branch_name: "b", merge_position: 0, merge_status: "pending", merge_task_id: null, blockers: [], updated_at: "", ...over });

describe("refactor campaign client", () => {
  it("parses campaigns with fallbacks, computes progress and skippability", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ campaign: { id: "c", status: "odd", shards: [{ id: "s1", merge_status: "weird", blockers: "no" }, { id: "s2", merge_status: "conflict", blockers: [{ kind: "merge_conflict", label: "x" }] }] } });
    const c = await client.getIssueCampaign("i1");
    expect(c?.status).toBe("running");
    expect(c?.shards[0]?.merge_status).toBe("pending");
    expect(c?.shards[0]?.blockers).toEqual([]);
    expect(c?.shards[1]?.blockers[0]?.kind).toBe("merge_conflict");
    stubFetch("garbage");
    expect(await client.getIssueCampaign("i1")).toBeNull();
    stubFetch({ campaign: { id: "c2", status: "running", shards: [] } }, 201);
    expect((await client.createCampaign({ issue_id: "i1", name: "n", target_branch: "main", leader_agent_id: "l", shards: [] }))?.id).toBe("c2");
    stubFetch({ campaign: { id: "c2", status: "completed", shards: [] } });
    expect((await client.skipCampaignShard("s1"))?.status).toBe("completed");
    const base = { id: "c", issue_id: "i", fanout_batch_id: "f", name: "n", target_branch: "main", status: "merging", created_at: "", completed_at: null } as RefactorCampaign;
    expect(campaignProgress({ ...base, shards: [shard({ merge_status: "merged" }), shard({ merge_status: "skipped" }), shard({ merge_status: "ready" }), shard({ merge_status: "conflict" })] })).toBe(0.5);
    expect(campaignProgress({ ...base, shards: [] })).toBe(0);
    expect(["pending", "ready", "conflict"].every((m) => campaignShardSkippable(shard({ merge_status: m as CampaignShard["merge_status"] })))).toBe(true);
    expect(["rebasing", "merged", "skipped"].some((m) => campaignShardSkippable(shard({ merge_status: m as CampaignShard["merge_status"] })))).toBe(false);
    expect(campaignKeys.issue("w", "i")).toEqual(["campaign", "w", "i"]);
  });
});
