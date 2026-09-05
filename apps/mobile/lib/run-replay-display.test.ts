import { describe, expect, it } from "vitest";

import type { RunReplayEvent } from "@/data/schemas";
import {
  previewJson,
  replayCountsSoFar,
  replayKindLabel,
  replaySealLabel,
} from "./run-replay-display";

function ev(seq: number, kind: string): RunReplayEvent {
  return {
    seq,
    at: "2026-09-04T00:00:00Z",
    kind,
    actor: { type: "agent", id: "a1", name: "Bot" },
    title: "",
    text: "",
    data: null,
    source: "",
    source_id: "",
    prev_hash: "",
    hash: "",
  };
}

describe("replayCountsSoFar", () => {
  const events = [
    ev(0, "status"),
    ev(1, "tool_use"),
    ev(2, "effect"),
    ev(3, "decision_asked"),
    ev(4, "steer"),
    ev(5, "tool_use"),
  ];

  it("is cumulative and inclusive of the current position", () => {
    expect(replayCountsSoFar(events, 1)).toEqual({
      toolCalls: 1,
      effects: 0,
      decisions: 0,
      steers: 0,
    });
    expect(replayCountsSoFar(events, 5)).toEqual({
      toolCalls: 2,
      effects: 1,
      decisions: 1,
      steers: 1,
    });
  });

  it("yields zeros before the first event and clamps past the end", () => {
    expect(replayCountsSoFar(events, -1)).toEqual({
      toolCalls: 0,
      effects: 0,
      decisions: 0,
      steers: 0,
    });
    expect(replayCountsSoFar(events, 99)).toEqual(replayCountsSoFar(events, 5));
  });
});

describe("replaySealLabel", () => {
  it("reads the server verdict, never re-verifies", () => {
    expect(replaySealLabel(null)).toBe("Not sealed yet");
    expect(
      replaySealLabel({ events: 3, head_hash: "h", sealed_at: "", verified: true }),
    ).toBe("Sealed and verified");
    expect(
      replaySealLabel({ events: 3, head_hash: "h", sealed_at: "", verified: false }),
    ).toBe("Seal broken");
  });
});

describe("replayKindLabel", () => {
  it("falls back to the wire value for a kind newer than this build", () => {
    expect(replayKindLabel("tool_use")).toBe("Tool call");
    expect(replayKindLabel("some_future_kind")).toBe("some_future_kind");
  });
});

describe("previewJson", () => {
  it("skips empty data and caps long objects with a marker line", () => {
    expect(previewJson(null)).toBe("");
    expect(previewJson({})).toBe("");
    const big = Object.fromEntries(
      Array.from({ length: 20 }, (_, i) => [`k${i}`, i]),
    );
    const lines = previewJson(big, 12).split("\n");
    expect(lines).toHaveLength(13);
    expect(lines[12]).toBe("…");
  });
});
