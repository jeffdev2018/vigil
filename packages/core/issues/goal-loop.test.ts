// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import type { AgentTask } from "../types";
import { goalLoopOfTask, goalOutcomeLabelKey } from "./goal-loop";

// Goal loop. Canonical layer for the endpoint's tolerance and the pure
// helpers; the views suite mounts the section, it does not re-run this matrix.

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
    ),
  );
}
afterEach(() => vi.unstubAllGlobals());

const client = () => new ApiClient("https://api.example.test");

const task = (result: unknown): AgentTask => ({
  id: "t1",
  agent_id: "a1",
  runtime_id: "r1",
  issue_id: "i1",
  status: "completed",
  priority: 0,
  dispatched_at: null,
  started_at: null,
  completed_at: null,
  result,
  error: null,
  created_at: "2026-09-05T00:00:00Z",
});

describe("getIssueGoal / setIssueGoal / pause / resume / answer", () => {
  it("parses a full goal state", async () => {
    stubFetch({
      goal: {
        id: "g1", issue_id: "i1", goal: "Ship the CSV export", status: "active",
        continuation: 2, max_continuations: 8, no_progress: 0, last_outcome: "continued",
        last_blocker: "", last_reason: "made progress", next_step: "write the parser",
        evidence: ["ran tests"], last_run_id: "t1", chain_root_task_id: "t0",
        set_by_type: "member", updated_at: "2026-09-05T00:00:00Z",
      },
    });
    const g = await client().getIssueGoal("i1");
    expect(g?.status).toBe("active");
    expect(g?.continuation).toBe(2);
    expect(g?.evidence).toEqual(["ran tests"]);
  });

  it("parses null goal and a pending question", async () => {
    stubFetch({ goal: null });
    expect(await client().getIssueGoal("i1")).toBeNull();
    stubFetch({
      goal: {
        id: "g1", issue_id: "i1", goal: "x", status: "waiting_user", continuation: 1,
        max_continuations: 5, no_progress: 1, last_outcome: "stopped:needs_user_input",
        evidence: [], set_by_type: "agent", updated_at: "2026-09-05T00:00:00Z",
        question: { kind: "choice", prompt: "Which env?", options: ["staging", "prod"], run_id: "t1", asked_at: "2026-09-05T00:00:00Z" },
      },
    });
    const g = await client().getIssueGoal("i1");
    expect(g?.question?.kind).toBe("choice");
    expect(g?.question?.options).toEqual(["staging", "prod"]);
  });

  // A malformed goal body must not take the issue panel down with it: the
  // fallback is a null goal, which the UI reads as "no goal set".
  it("falls back to a null goal on garbage, and defaults a partial one", async () => {
    stubFetch("not an object");
    expect(await client().getIssueGoal("i1")).toBeNull();
    stubFetch({ goal: { id: "g1", status: "sideways", continuation: "two", max_continuations: null } });
    const g = await client().getIssueGoal("i1");
    expect(g?.status).toBe("active");
    expect(g?.continuation).toBe(0);
    expect(g?.max_continuations).toBe(1);
    expect(g?.set_by_type).toBe("system");
  });

  it("sends the write body and parses the mutation responses", async () => {
    stubFetch({ goal: { id: "g1", issue_id: "i1", goal: "x", status: "active", continuation: 0, max_continuations: 3, no_progress: 0, last_outcome: "", evidence: [], set_by_type: "member", updated_at: "" } });
    const c = client();
    const g = await c.setIssueGoal("i1", { goal: "x", max_continuations: 3 });
    expect(g?.max_continuations).toBe(3);
    stubFetch({ goal: { id: "g1", status: "paused" } });
    expect((await c.pauseIssueGoal("i1"))?.status).toBe("paused");
    stubFetch({ goal: { id: "g1", status: "active" } });
    expect((await c.resumeIssueGoal("i1"))?.status).toBe("active");
    stubFetch({ goal: { id: "g1", status: "active", question: { kind: "text", prompt: "?", run_id: "t1", asked_at: "", answer: "yes" } } });
    expect((await c.answerIssueGoal("i1", "yes"))?.question?.answer).toBe("yes");
  });
});

describe("goalLoopOfTask", () => {
  it("parses a run's contribution to the loop", () => {
    const v = goalLoopOfTask(task({ goal_loop: { continuation: 3, signature: "sig1", no_progress: 1, outcome: "continued", next_step: "write tests" } }));
    expect(v?.continuation).toBe(3);
    expect(v?.next_step).toBe("write tests");
  });

  it("is null for a run with no goal_loop key, a non-object result, and malformed JSON", () => {
    expect(goalLoopOfTask(task({ other: 1 }))).toBeNull();
    expect(goalLoopOfTask(task(null))).toBeNull();
    expect(goalLoopOfTask(task("garbage"))).toBeNull();
    // A garbage goal_loop still resolves to a defaulted verdict, not null —
    // same tolerance rule as every other embedded blob in this codebase.
    const v = goalLoopOfTask(task({ goal_loop: { continuation: "two" } }));
    expect(v?.continuation).toBe(0);
    expect(v?.outcome).toBe("");
  });
});

describe("goalOutcomeLabelKey", () => {
  it("maps every documented outcome", () => {
    expect(goalOutcomeLabelKey("")).toBe("none");
    expect(goalOutcomeLabelKey("satisfied")).toBe("satisfied");
    expect(goalOutcomeLabelKey("continued")).toBe("continued");
    for (const why of ["exhausted", "stagnation", "needs_user_input", "external_wait", "run_failed", "judge_unavailable", "paused", "issue_changed"]) {
      expect(goalOutcomeLabelKey(`stopped:${why}`)).toBe(`stopped_${why}`);
    }
  });

  it("folds an unknown stop reason and an unknown outcome shape to fallbacks", () => {
    expect(goalOutcomeLabelKey("stopped:time_travel")).toBe("stopped_unknown");
    expect(goalOutcomeLabelKey("sideways")).toBe("other");
  });
});
