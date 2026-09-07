// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { inboxDecisionsOptions } from "../inbox/decisions";
const api = new ApiClient("https://api.example.test");
const id = "11111111-1111-4111-8111-111111111111";
const other = "22222222-2222-4222-8222-222222222222";
const row = { id, issue_id: id, agent_id: id, source_task_id: id, recipient_id: id, requested_by: id,
  requester_type: "agent", question: "A or B?", context: "Tradeoffs", options: ["A", "B"], status: "open", answer: null,
  answered_by: null, answered_at: null, resume_task_id: null, created_at: "2026-09-06T00:00:00Z" };
const answered = { ...row, status: "answered", answer: "A", answered_by: id, answered_at: row.created_at };
function stub(body: unknown) {
  const fetch = vi.fn().mockImplementation(async () => new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } }));
  vi.stubGlobal("fetch", fetch); return fetch;
}
afterEach(() => vi.unstubAllGlobals());
it("scopes active/history queries and preserves durable decision identity", async () => {
  const fetch = stub({ decisions: [row], next_before_id: id });
  expect(await api.listIssueDecisions(true, id)).toMatchObject({ decisions: [{ issueId: id, sourceTaskId: id, status: "open" }], nextBeforeId: id });
  expect(fetch.mock.calls[0]?.[0]).toBe(`https://api.example.test/api/inbox/decisions?history=true&before_id=${id}`);
  expect(inboxDecisionsOptions("a").queryKey).not.toEqual(inboxDecisionsOptions("b").queryKey);
  expect(inboxDecisionsOptions("a").queryKey).not.toEqual(inboxDecisionsOptions("a",true).queryKey);
});
it("requires the exact answered decision and resume receipt", async () => {
  const fetch = stub(answered);
  expect(await api.answerIssueDecision(id,id,"answered","A")).toMatchObject({ status: "answered", answer: "A" });
  expect(JSON.parse(fetch.mock.calls[0]![1].body)).toEqual({status:"answered",answer:"A"});
  for (const body of [{...answered,id:other},{...answered,issue_id:other},{...answered,answer:"B"},{...answered,answered_by:other}]) {
    stub(body); expect(await api.answerIssueDecision(id,id,"answered","A")).toBeNull();
  }
  stub(answered); expect(await api.resumeIssueDecision(id,id)).toBeNull();
  stub({...answered,resume_task_id:other}); expect(await api.resumeIssueDecision(id,id)).toMatchObject({resumeTaskId:other});
});
it("never interprets malformed or future responses as successful decisions", async () => {
  for (const decision of [{}, {...row,status:"future"}, {...row,answer:"A"}, {...row,resume_task_id:id}, {...answered,answer:null}, {...row,status:"cancelled"}]) {
    stub({decisions:[decision],next_before_id:null}); expect(await api.listIssueDecisions()).toBeNull();
  }
  stub({decisions:[],next_before_id:4}); expect(await api.listIssueDecisions()).toBeNull();
});
