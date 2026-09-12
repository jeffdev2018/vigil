import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import { AgentConsultListSchema, type AgentConsult } from "./schemas";

describe("agent consult API schema", () => {
  it("keeps a consult with an unknown state instead of dropping the run's lines", () => {
    const rows = parseWithFallback(
      [{
        consult_id: "c1",
        task_id: "t1",
        agent_id: "a1",
        model: "m",
        question: "q",
        answer: null,
        state: "streaming",
        refusal_reason: null,
        input_tokens: null,
        output_tokens: null,
        cost_usd_ticks: null,
        created_at: "2026-09-01T00:00:00Z",
      }],
      AgentConsultListSchema,
      [] as AgentConsult[],
      { endpoint: "test" },
    );
    expect(rows).toHaveLength(1);
    expect(rows[0]?.state).toBe("streaming");
  });

  it("falls back on a malformed consult row", () => {
    expect(
      parseWithFallback([{ consult_id: "" }], AgentConsultListSchema, [], { endpoint: "test" }),
    ).toEqual([]);
  });
});
