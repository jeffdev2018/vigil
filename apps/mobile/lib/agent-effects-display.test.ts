import { describe, expect, it } from "vitest";
import type { AgentEffect, UndoReport } from "@/data/schemas";
import {
  describeEffect,
  effectState,
  groupEffectsByRun,
  undoReportLines,
} from "./agent-effects-display";

function effect(over: Partial<AgentEffect>): AgentEffect {
  return {
    id: "e1",
    task_id: "t1",
    agent_id: "a1",
    agent_name: "Ada",
    issue_id: "i1",
    kind: "issue_status",
    target_type: "issue",
    target_id: "i1",
    before: { value: "todo" },
    after: { value: "done" },
    reversible: true,
    status: "applied",
    decision_id: null,
    payload: {},
    reversed_at: null,
    reversed_by_type: null,
    reverse_error: null,
    within_window: true,
    expires_at: "",
    created_at: "",
    ...over,
  };
}

describe("effectState", () => {
  it("mirrors core effectState precedence", () => {
    expect(effectState(effect({}))).toBe("pending");
    expect(effectState(effect({ status: "pending" }))).toBe("held");
    expect(effectState(effect({ status: "approved" }))).toBe("approved");
    expect(effectState(effect({ status: "rejected" }))).toBe("rejected");
    expect(effectState(effect({ reversed_at: "2026-01-01" }))).toBe("reversed");
    expect(effectState(effect({ reversible: false }))).toBe("not_reversible");
    expect(effectState(effect({ reverse_error: "boom" }))).toBe("failed");
    expect(effectState(effect({ within_window: false }))).toBe("expired");
  });
});

describe("groupEffectsByRun", () => {
  it("groups by task in list order and counts pending", () => {
    const runs = groupEffectsByRun([
      effect({ id: "1", task_id: "t2", agent_name: "Bob" }),
      effect({ id: "2", task_id: "t1" }),
      effect({ id: "3", task_id: "t2", within_window: false }),
    ]);
    expect(runs.map((r) => r.task_id)).toEqual(["t2", "t1"]);
    expect(runs[0]?.agent_name).toBe("Bob");
    expect(runs[0]?.effects.map((e) => e.id)).toEqual(["1", "3"]);
    expect(runs[0]?.pending).toBe(1);
  });
});

describe("describeEffect", () => {
  it("labels every kind and falls back to the raw kind", () => {
    expect(describeEffect(effect({}))).toBe("Status todo → done");
    expect(
      describeEffect(
        effect({ kind: "issue_field", before: { field: "assignee" }, after: {} }),
      ),
    ).toBe("Assignee changed");
    expect(
      describeEffect(
        effect({ kind: "issue_field", before: { field: "priority" }, after: { value: "high" } }),
      ),
    ).toBe("priority: ∅ → high");
    expect(
      describeEffect(effect({ kind: "comment_create", after: { excerpt: "hi" } })),
    ).toBe("Comment: hi");
    expect(describeEffect(effect({ kind: "comment_update" }))).toBe("Comment edited");
    expect(
      describeEffect(effect({ kind: "note_create", after: { title: "N" } })),
    ).toBe("Note created: N");
    expect(
      describeEffect(effect({ kind: "note_update", before: { title: "Old" }, after: {} })),
    ).toBe("Note edited: Old");
    expect(describeEffect(effect({ kind: "note_archive" }))).toBe("Note archived");
    expect(
      describeEffect(effect({ kind: "triage_verdict", after: { verdict: "accept" } })),
    ).toBe("Triage verdict: accept");
    expect(
      describeEffect(effect({ kind: "issue_create", after: { title: "T" } })),
    ).toBe("Issue created: T");
    expect(
      describeEffect(effect({ kind: "issue_update", payload: { title: 1, status: 2 } })),
    ).toBe("Issue update: title, status");
    expect(describeEffect(effect({ kind: "something_new" }))).toBe("something_new");
  });
});

describe("undoReportLines", () => {
  const base: UndoReport = {
    reversed: 0,
    skipped: [],
    breaker: { tripped: false, trust_mode: "" },
    effects: [],
  };
  it("is empty when nothing happened", () => {
    expect(undoReportLines(base)).toEqual([]);
  });
  it("reports reversed, first skip reason and breaker", () => {
    expect(
      undoReportLines({
        ...base,
        reversed: 2,
        skipped: [{ id: "x", kind: "k", reason: "reverse_failed: db" }],
        breaker: { tripped: true, trust_mode: "preview" },
      }),
    ).toEqual([
      "2 change(s) reversed",
      "1 change(s) skipped: reversal failed",
      "Breaker tripped: the agent now runs in preview mode",
    ]);
  });
});
