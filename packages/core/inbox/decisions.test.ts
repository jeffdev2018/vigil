// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { inboxKeys } from "./queries";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal("fetch", vi.fn().mockImplementation(() => Promise.resolve(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }))));
}
afterEach(() => vi.unstubAllGlobals());

describe("inbox decisions client (K63)", () => {
  it("parses the capped list with its total and tolerates drift", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ decisions: [{ inbox_item_id: "ib", issue_id: "i", issue_identifier: "ACME-1", risk_score: "x", decision: { id: "d", question: "Q?", options: "bad", urgency: "high" } }], total: 7 });
    const out = await client.listInboxDecisions();
    expect(out.total).toBe(7);
    expect(out.decisions[0]?.risk_score).toBe(0);
    expect(out.decisions[0]?.decision?.options).toEqual([]);
    stubFetch("garbage");
    expect(await client.listInboxDecisions()).toEqual({ decisions: [], total: 0 });
    expect(inboxKeys.decisions("w")).toEqual(["inbox", "w", "decisions"]);
  });

  it("defaults a source-less entry (pre-JEF-244 server) to a Decision Card", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ decisions: [{ inbox_item_id: "ib", issue_id: "i", issue_identifier: "ACME-1", issue_title: "T", risk_score: 1, decision: { id: "d", question: "Q?", options: [] } }], total: 1 });
    const entry = (await client.listInboxDecisions()).decisions[0];
    expect(entry?.source).toBe("decision");
    expect(entry?.decision?.id).toBe("d");
    expect(entry?.transition).toBeNull();
    expect(entry?.goal_question).toBeNull();
  });

  it("parses transition and goal-question entries (JEF-244)", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({
      decisions: [
        { inbox_item_id: "ib-1", issue_id: "i-1", issue_identifier: "ACME-1", issue_title: "T1", risk_score: 3, source: "transition", transition: { request_id: "tr-1", from_status: "Backlog", to_status: "In Progress", rule_id: null, approver_roles: ["owner"] } },
        { inbox_item_id: "ib-2", issue_id: "i-2", issue_identifier: "ACME-2", issue_title: "T2", risk_score: 2, source: "goal_question", goal_question: { kind: "choice", prompt: "Which env?", options: ["staging", "prod"], run_id: "run-1", asked_at: "2026-09-11T00:00:00Z" } },
      ],
      total: 2,
    });
    const out = await client.listInboxDecisions(["transitions", "goal_questions"]);
    expect(out.decisions[0]?.source).toBe("transition");
    expect(out.decisions[0]?.decision).toBeNull();
    expect(out.decisions[0]?.transition).toMatchObject({ request_id: "tr-1", from_status: "Backlog", to_status: "In Progress", rule_id: null, approver_roles: ["owner"] });
    expect(out.decisions[1]?.source).toBe("goal_question");
    expect(out.decisions[1]?.goal_question).toMatchObject({ kind: "choice", prompt: "Which env?", options: ["staging", "prod"], run_id: "run-1" });
  });

  it("sends the include param only when asked, and keys the cache on it", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ decisions: [], total: 0 });
    await client.listInboxDecisions(["transitions", "goal_questions"]);
    expect(vi.mocked(fetch).mock.calls[0]?.[0]).toBe("https://api.example.test/api/inbox/decisions?include=transitions%2Cgoal_questions");
    await client.listInboxDecisions();
    expect(vi.mocked(fetch).mock.calls[1]?.[0]).toBe("https://api.example.test/api/inbox/decisions");
    expect(inboxKeys.decisions("w", ["transitions", "goal_questions"])).toEqual(["inbox", "w", "decisions", "transitions,goal_questions"]);
  });
});
