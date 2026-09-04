// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { isCriterionSatisfied, unsatisfiedCriteria } from "./acceptance";
import type { AcceptanceCriterion } from "../types";

function stubFetchJson(body: unknown, status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
    ),
  );
}

afterEach(() => vi.unstubAllGlobals());

const missing: AcceptanceCriterion = { id: "a", text: "Tests pass", proof_state: "missing" };
const proven: AcceptanceCriterion = { id: "b", text: "Reviewed", proof_type: "human_validation", proof_state: "satisfied", validated_by: "u1" };

describe("acceptance helpers", () => {
  it("trusts only the server's satisfied state", () => {
    expect(isCriterionSatisfied(proven)).toBe(true);
    expect(isCriterionSatisfied(missing)).toBe(false);
    expect(isCriterionSatisfied({ proof_state: "pending_human" })).toBe(false);
    expect(unsatisfiedCriteria([missing, proven])).toEqual([missing]);
  });
});

describe("acceptance endpoints", () => {
  it("keeps an unknown proof type or state as text and defaults a missing state", async () => {
    stubFetchJson({ criteria: [{ id: "a", text: "x", proof_type: "notarized", proof_state: "weird" }, { id: "b" }] });
    const list = await new ApiClient("https://api.example.test").listAcceptanceCriteria("i1");
    expect(list.map((c) => [c.id, c.proof_type, c.proof_state, c.text])).toEqual([
      ["a", "notarized", "weird", "x"],
      ["b", undefined, "missing", ""],
    ]);
  });

  it("falls back to an empty list on a malformed body", async () => {
    stubFetchJson({ criteria: "nope" });
    expect(await new ApiClient("https://api.example.test").listAcceptanceCriteria("i1")).toEqual([]);
    stubFetchJson({ criteria: [{ id: 4 }] });
    expect(await new ApiClient("https://api.example.test").proveAcceptanceCriterion("i1", "a", { proof_type: "url", proof_ref: "https://x" })).toEqual([]);
  });
});
