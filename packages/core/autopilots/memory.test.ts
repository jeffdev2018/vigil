// @vitest-environment node
import { describe, expect, it } from "vitest";
import { AutopilotMemorySchema, EMPTY_AUTOPILOT_MEMORY, hasAutopilotMemory } from "./memory";

describe("AutopilotMemorySchema", () => {
  it("keeps a well-formed memory intact", () => {
    const parsed = AutopilotMemorySchema.parse({
      autopilot_id: "ap-1",
      content: "The queue label is inbound.",
      revision: 3,
      updated_by_task_id: "task-1",
      updated_at: "2026-09-04T10:00:00Z",
    });
    expect(parsed.content).toBe("The queue label is inbound.");
    expect(parsed.revision).toBe(3);
    expect(parsed.updated_by_task_id).toBe("task-1");
  });

  it("accepts a memory no run has written yet", () => {
    const parsed = AutopilotMemorySchema.parse({ autopilot_id: "ap-1", content: "", revision: 0, updated_by_task_id: null });
    expect(parsed.revision).toBe(0);
    expect(parsed.updated_by_task_id).toBeNull();
  });

  it("defaults every field a partial response omits", () => {
    const parsed = AutopilotMemorySchema.parse({});
    expect(parsed).toMatchObject({ autopilot_id: "", content: "", revision: 0, updated_at: "" });
  });

  it("refuses a response whose content is not a string", () => {
    // Degrading a number to "0" here would put a fake memory in front of
    // someone deciding whether to trust what the daemon knows.
    expect(AutopilotMemorySchema.safeParse({ content: 42 }).success).toBe(false);
  });
});

describe("EMPTY_AUTOPILOT_MEMORY", () => {
  it("is what an unreadable response degrades to: the empty state", () => {
    expect(hasAutopilotMemory(EMPTY_AUTOPILOT_MEMORY)).toBe(false);
  });
});

describe("hasAutopilotMemory", () => {
  it("treats whitespace-only content as empty", () => {
    // The server stores what a run sent; a run that wrote "\n  \n" told the
    // next one nothing, and the card must show the empty state, not a blank box.
    expect(hasAutopilotMemory({ ...EMPTY_AUTOPILOT_MEMORY, content: "\n  \t\n" })).toBe(false);
  });

  it("is true for real content", () => {
    expect(hasAutopilotMemory({ ...EMPTY_AUTOPILOT_MEMORY, content: "a note" })).toBe(true);
  });

  it("handles an undefined memory while the query is loading", () => {
    expect(hasAutopilotMemory(undefined)).toBe(false);
  });
});
