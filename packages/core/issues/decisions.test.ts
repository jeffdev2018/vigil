// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { decisionAnswerLabel, isDecisionPending, pendingDecisions } from "./decisions";
import type { IssueDecision } from "../types";

function stubFetchJson(body: unknown, status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
    ),
  );
}

afterEach(() => vi.unstubAllGlobals());

const card: IssueDecision = {
  id: "d1", issue_id: "i1", asked_by_type: "agent", asked_by_id: "a", question: "Drop it?",
  options: [{ id: "drop", label: "Drop it" }, { id: "keep", label: "Keep" }],
  recommended_option_id: "keep", urgency: "high", response: null, responded_at: null, created_at: "2026-09-03T00:00:00Z",
};

describe("decision helpers", () => {
  it("tells pending from answered and labels the answer", () => {
    expect(isDecisionPending(card)).toBe(true);
    const chosen = { ...card, response: { option_id: "drop" }, responded_at: "x" };
    expect(isDecisionPending(chosen)).toBe(false);
    expect(decisionAnswerLabel(chosen)).toBe("Drop it");
    expect(decisionAnswerLabel({ ...card, response: { modified_text: "Archive" }, responded_at: "x" })).toBe("Archive");
    expect(decisionAnswerLabel({ ...card, response: { option_id: "gone" }, responded_at: "x" })).toBe("gone");
    expect(pendingDecisions([card, chosen])).toEqual([card]);
  });
});

describe("decision endpoints", () => {
  it("parses a list, keeping an unknown urgency, and drops malformed cards", async () => {
    stubFetchJson({ decisions: [{ ...card, urgency: "critical" }, { id: 4 }] });
    const list = await new ApiClient("https://api.example.test").listIssueDecisions("i1");
    // A malformed element fails the array: the fallback is an empty list.
    expect(Array.isArray(list)).toBe(true);
    stubFetchJson({ decisions: [{ ...card, urgency: "critical" }] });
    const ok = await new ApiClient("https://api.example.test").listIssueDecisions("i1");
    expect(ok[0]?.urgency).toBe("critical");
  });

  it("falls back to an empty list on a malformed body and rejects a malformed answer", async () => {
    stubFetchJson({ decisions: "nope" });
    expect(await new ApiClient("https://api.example.test").listIssueDecisions("i1")).toEqual([]);
    stubFetchJson({ decision: { id: 1 } });
    await expect(new ApiClient("https://api.example.test").respondIssueDecision("i1", "d1", { option_id: "drop" })).rejects.toThrow();
  });
});
