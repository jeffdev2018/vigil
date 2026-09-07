// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { AgentMemorySchema } from "../api/schemas";
afterEach(() => vi.unstubAllGlobals());

it("requires correction provenance on creation and rejects unreadable evidence", async () => {
 const id = "11111111-1111-4111-8111-111111111111";
 const source = {review_id:id,issue_id:id,task_id:id,feedback:"Keep project context",criteria:["Open the right form"],assessments:[{passed:false,evidence:"Project was lost"}],snapshot_token:"a".repeat(64),reviewed_by:id,reviewed_at:"2026-09-05T00:00:00Z"};
 const row = {id:"memory",agent_id:"agent",content:"Preserve project context",status:"pending",revision:1,source_task_id:id,source_review:source};
 const fetch = vi.fn().mockImplementation(() => Promise.resolve(new Response(JSON.stringify(row))));
 vi.stubGlobal("fetch",fetch);
 const client = new ApiClient("http://memory.test");
 expect((await client.createAgentMemory("agent",row.content,id,null,id)).source_review?.feedback).toBe(source.feedback);
 expect(fetch).toHaveBeenLastCalledWith("http://memory.test/api/agents/agent/memories",expect.objectContaining({body:JSON.stringify({content:row.content,source_task_id:id,expires_at:null,source_review_id:id})}));
 for (const extra of [{source_review:null}, {source_task_id:"foreign"}, {source_review:{...source,review_id:"22222222-2222-4222-8222-222222222222"}}, {source_review:{...source,assessments:"invalid"}}, {status:"unknown"}]) {
   fetch.mockImplementation(() => Promise.resolve(new Response(JSON.stringify({...row,...extra}))));
   await expect(client.createAgentMemory("agent",row.content,id,null,id)).rejects.toThrow(/Invalid .*memory response/);
 }
 fetch.mockImplementation(() => Promise.resolve(new Response(JSON.stringify([{...row,source_review:{...source,reviewed_at:"yesterday"}}]))));
 await expect(client.listAgentMemories("agent")).rejects.toThrow("Invalid agent memory response");
});
it("preserves expiration and rejects unreadable memory instead of showing an empty list", async () => {
 const row = { id: "memory", agent_id: "agent", content: "Temporary", expires_at: "2026-01-01T00:00:00Z", expired: true };
 expect(AgentMemorySchema.parse(row).expired).toBe(true);
 for (const extra of [{ expires_at: 42 }, { expires_at: "tomorrow" }, { expired: "false" }]) expect(AgentMemorySchema.safeParse({ ...row, ...extra }).success).toBe(false);
 const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify([ { ...row, expires_at: "bad" } ]), { status: 200 }));
 vi.stubGlobal("fetch", fetch);
 const client = new ApiClient("http://memory.test");
 await expect(client.listAgentMemories("agent")).rejects.toThrow("Invalid agent memory response");
 fetch.mockResolvedValue(new Response(JSON.stringify(row), { status: 200 }));
 await client.updateAgentMemory("agent", "memory", { expires_at: null, expected_revision: 3 });
 expect(fetch).toHaveBeenLastCalledWith("http://memory.test/api/agents/agent/memories/memory", expect.objectContaining({ body: JSON.stringify({ expires_at: null, expected_revision: 3 }) }));
 fetch.mockImplementation(() => Promise.resolve(new Response(JSON.stringify({ ...row, expires_at: "invalid" }), { status: 200 })));
 await expect(client.createAgentMemory("agent", "Temporary")).rejects.toThrow("Invalid agent memory response");
 await expect(client.updateAgentMemory("agent", "memory", { expires_at: null, expected_revision: 3 })).rejects.toThrow("Invalid agent memory response");

});

it("validates memory history and sends the selected revision without altering the snapshot", async () => {
  const version = { id: "memory", agent_id: "agent", content: "Original", revision: 2, status: "pending", expires_at: null };
  const fetch = vi.fn().mockImplementation(() => Promise.resolve(new Response(JSON.stringify({ versions: [version], next_before_revision: 2 }), { status: 200 })));
  vi.stubGlobal("fetch", fetch);
  const client = new ApiClient("http://memory.test");
  expect((await client.getAgentMemoryHistory("agent", "memory", 3)).versions[0]?.revision).toBe(2);
  expect(fetch).toHaveBeenLastCalledWith("http://memory.test/api/agents/agent/memories/memory/history?before_revision=3", expect.anything());
  for (const malformed of [{ versions: [ { ...version, revision: 0 } ], next_before_revision: null }, { versions: [ { ...version, agent_id: "foreign" } ], next_before_revision: null }, { versions: [], next_before_revision: "bad" }]) {
    fetch.mockImplementation(() => Promise.resolve(new Response(JSON.stringify(malformed), { status: 200 })));
    await expect(client.getAgentMemoryHistory("agent", "memory")).rejects.toThrow("Invalid agent memory history response");
  }
  fetch.mockImplementation(() => Promise.resolve(new Response(JSON.stringify({ ...version, revision: 3 }), { status: 200 })));
  await client.updateAgentMemory("agent", "memory", { restore_revision: 1, expected_revision: 2 });
  expect(fetch).toHaveBeenLastCalledWith("http://memory.test/api/agents/agent/memories/memory", expect.objectContaining({ body: JSON.stringify({ restore_revision: 1, expected_revision: 2 }) }));
  fetch.mockImplementation(() => Promise.resolve(new Response(JSON.stringify({ id: "memory", agent_id: "agent" }), { status: 200 })));
  await expect(client.updateAgentMemory("agent", "memory", { restore_revision: 1, expected_revision: 2 })).rejects.toThrow("Invalid restored memory response");
});
