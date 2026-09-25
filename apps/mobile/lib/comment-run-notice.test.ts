import { describe, expect, it } from "vitest";
import { agentRunNoticeText, commentTriggerSignature } from "./comment-run-notice";

// Audit UX (sept. 2026): mentioning an agent in a mobile comment started a
// real run with no warning. The composer now asks the server which agents a
// send would start (same preview as web) and says so with runtime and cost.
describe("commentTriggerSignature", () => {
  it("changes with the mention set and emptiness only, so typing does not refetch", () => {
    expect(commentTriggerSignature("  ")).toBe("empty");
    expect(commentTriggerSignature("/note just for me")).toBe("empty");
    expect(commentTriggerSignature("hello")).toBe(commentTriggerSignature("hello there"));
    const withAgent = commentTriggerSignature("[@Walt](mention://agent/a1) fix it");
    expect(withAgent).toBe("nonempty|agent:a1");
    expect(commentTriggerSignature("[MUL-1](mention://issue/i1) x")).toBe("nonempty|");
  });
});

describe("agentRunNoticeText", () => {
  it("names the agent, the runtime and the recent average cost", () => {
    expect(agentRunNoticeText({ name: "Walt", runtimeName: "Studio", avgCostUsdTicks: 25_000_000_000, sampleRuns: 3, costPending: false }))
      .toBe("Walt will start a run when you send · runs on Studio · ≈ $2.50 per run (average of its last 3 priced runs)");
  });

  it("says cost unknown instead of inventing a figure", () => {
    expect(agentRunNoticeText({ name: "Walt", runtimeName: null, avgCostUsdTicks: null, sampleRuns: 0, costPending: false }))
      .toBe("Walt will start a run when you send · runs on a runtime you cannot see · cost unknown (no priced run yet)");
  });
});
