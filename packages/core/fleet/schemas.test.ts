import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import {
  AgentConsultListSchema,
  FleetCostListSchema,
  FleetHistoryListSchema,
  FleetStatusListSchema,
  type AgentConsult,
  type FleetStatusRow,
} from "./schemas";

describe("fleet API schemas", () => {
  it("falls back instead of leaking malformed status rows", () => {
    expect(
      parseWithFallback([{ agent_id: "a1", running_task_count: -1 }], FleetStatusListSchema, [], { endpoint: "test" }),
    ).toEqual([]);
  });

  it("falls back on malformed cost rows (negative ticks)", () => {
    expect(
      parseWithFallback([{ agent_id: "a1", cost_usd_ticks: -5, input_tokens: 0, output_tokens: 0, task_count: 1 }], FleetCostListSchema, [], { endpoint: "test" }),
    ).toEqual([]);
  });

  it("falls back on malformed history rows", () => {
    expect(
      parseWithFallback([{ date: 42 }], FleetHistoryListSchema, [], { endpoint: "test" }),
    ).toEqual([]);
  });

  it("parses a well-formed fleet status list, defaulting a missing name", () => {
    const rows = parseWithFallback(
      [{ agent_id: "a1", running_task_count: 2, task_count: 9, failed_count: 1 }],
      FleetStatusListSchema,
      [] as FleetStatusRow[],
      { endpoint: "test" },
    );
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({ agent_id: "a1", name: "", failed_count: 1 });
  });

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
