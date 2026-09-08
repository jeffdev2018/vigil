// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import {
  EMPTY_EPIC,
  EpicSchema,
  epicAppliedKeys,
  epicRailState,
  epicRemainingTickets,
  epicStep,
  epicTickets,
  nextEnabledStep,
  stepLockedBy,
  type Epic,
  type EpicArtifact,
  type EpicStep,
} from "./epic";
import { parseWithFallback } from "../api/schema";

// Canonical layer for the Epic Mode contract (F18). The component suite
// (packages/views/projects/components/epic-panel.test.tsx) keeps the happy
// path and the wiring; the parsing and gate matrices live here.

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
    ),
  );
}
afterEach(() => vi.unstubAllGlobals());

const artifact = (over: Partial<EpicArtifact> = {}): EpicArtifact => ({
  id: "a1",
  project_id: "p1",
  kind: "prd",
  version: 1,
  content: "# PRD",
  payload: {},
  state: "draft",
  author_type: "member",
  author_id: "u1",
  approved_by: "",
  approved_at: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  generated_by_task_id: "",
  ...over,
});

const step = (over: Partial<EpicStep> = {}): EpicStep => ({
  kind: "prd",
  latest: null,
  approved: null,
  generating: false,
  ...over,
});

const epic = (steps: Record<string, EpicStep>): Epic => ({
  steps,
  epic_issue_id: "i1",
  next_kind: "",
});

describe("EpicSchema", () => {
  it("parses a well-formed pipeline", () => {
    const parsed = parseWithFallback(
      {
        steps: { prd: { kind: "prd", latest: artifact({ state: "approved" }), approved: artifact({ state: "approved" }), generating: false } },
        epic_issue_id: "i1",
        next_kind: "tech_plan",
      },
      EpicSchema,
      EMPTY_EPIC,
      { endpoint: "test" },
    );
    expect(parsed.next_kind).toBe("tech_plan");
    expect(parsed.steps.prd?.approved?.content).toBe("# PRD");
  });

  it("falls back to an empty pipeline on a malformed answer", () => {
    for (const bad of ["nope", 42, null, []]) {
      expect(parseWithFallback(bad, EpicSchema, EMPTY_EPIC, { endpoint: "test" })).toEqual(EMPTY_EPIC);
    }
  });

  it("recovers a response whose steps map is the wrong shape", () => {
    // The object still parses — every field catches — so the panel renders an
    // empty rail rather than losing the epic_issue_id it can still use.
    expect(parseWithFallback({ steps: "not an object", epic_issue_id: "i1" }, EpicSchema, EMPTY_EPIC, { endpoint: "test" }))
      .toEqual({ steps: {}, epic_issue_id: "i1", next_kind: "" });
  });

  it("keeps a state and a kind this build does not know", () => {
    const parsed = parseWithFallback(
      { steps: { prd: { kind: "prd", latest: { ...artifact(), state: "archived" }, approved: null, generating: false } }, epic_issue_id: "", next_kind: "" },
      EpicSchema,
      EMPTY_EPIC,
      { endpoint: "test" },
    );
    expect(parsed.steps.prd?.latest?.state).toBe("archived");
  });

  it("defaults the fields a partial artifact omits instead of dropping the row", () => {
    const parsed = parseWithFallback(
      { steps: { prd: { kind: "prd", latest: { id: "a1", content: "# PRD" }, generating: false } } },
      EpicSchema,
      EMPTY_EPIC,
      { endpoint: "test" },
    );
    expect(parsed.steps.prd?.latest).toMatchObject({ id: "a1", content: "# PRD", version: 0, state: "draft" });
    expect(parsed.steps.prd?.approved).toBeNull();
  });
});

describe("the gate", () => {
  it("never locks the first step", () => {
    expect(stepLockedBy(epic({}), "prd")).toBeNull();
    expect(stepLockedBy(undefined, "prd")).toBeNull();
  });

  it("locks a step on its immediate predecessor's approval, not on a draft", () => {
    expect(stepLockedBy(epic({}), "tech_plan")).toBe("prd");
    expect(stepLockedBy(epic({ prd: step({ latest: artifact() }) }), "tech_plan")).toBe("prd");
    expect(stepLockedBy(epic({ prd: step({ approved: artifact({ state: "approved" }) }) }), "tech_plan")).toBeNull();
  });

  it("walks the pipeline one approval at a time", () => {
    let state = epic({});
    expect(nextEnabledStep(state)).toBe("prd");
    for (const [approved, expected] of [
      ["prd", "tech_plan"],
      ["tech_plan", "wireframe"],
      ["wireframe", "tickets"],
      ["tickets", ""],
    ] as const) {
      state = epic({ ...state.steps, [approved]: step({ kind: approved, approved: artifact({ state: "approved" }) }) });
      expect(nextEnabledStep(state)).toBe(expected);
    }
  });

  it("points back at a reopened step even when later ones were approved once", () => {
    // The server supersedes later approvals on an edit, so this is what a
    // reopened pipeline actually looks like on the wire.
    const state = epic({
      prd: step({ kind: "prd", latest: artifact({ version: 2 }) }),
      tech_plan: step({ kind: "tech_plan", latest: artifact({ kind: "tech_plan", state: "superseded" }) }),
    });
    expect(nextEnabledStep(state)).toBe("prd");
    expect(stepLockedBy(state, "tech_plan")).toBe("prd");
  });

  it("treats an unknown kind as locked by nothing and out of the pipeline", () => {
    expect(stepLockedBy(epic({}), "roadmap")).toBeNull();
    expect(epicStep(epic({}), "roadmap")).toMatchObject({ kind: "roadmap", latest: null, generating: false });
  });
});

describe("epicRailState", () => {
  it("maps every state the rail renders", () => {
    expect(epicRailState(step())).toBe("empty");
    expect(epicRailState(step({ generating: true }))).toBe("generating");
    // Generating wins over a stale artifact: a run is out on top of it.
    expect(epicRailState(step({ generating: true, latest: artifact({ state: "approved" }) }))).toBe("generating");
    expect(epicRailState(step({ latest: artifact({ state: "draft" }) }))).toBe("draft");
    expect(epicRailState(step({ latest: artifact({ state: "approved" }) }))).toBe("approved");
    expect(epicRailState(step({ latest: artifact({ state: "superseded" }) }))).toBe("superseded");
  });

  it("names an unrecognised state rather than guessing which one it behaves like", () => {
    expect(epicRailState(step({ latest: artifact({ state: "archived" }) }))).toBe("unknown");
    expect(epicRailState(step({ latest: artifact({ state: "" }) }))).toBe("unknown");
  });
});

describe("tickets", () => {
  const withTickets = (payload: unknown) => artifact({ kind: "tickets", payload });

  it("reads a well-formed breakdown", () => {
    const tickets = epicTickets(withTickets({
      tickets: [
        { external_key: "T1", title: "Add it", description: "…", depends_on: [] },
        { external_key: "T2", title: "Test it", description: "…", depends_on: ["T1"] },
      ],
    }));
    expect(tickets.map((t) => t.external_key)).toEqual(["T1", "T2"]);
    expect(tickets[1]?.depends_on).toEqual(["T1"]);
  });

  it("survives a payload that is not a breakdown", () => {
    for (const bad of [undefined, null, {}, { tickets: "nope" }, { tickets: null }]) {
      expect(epicTickets(withTickets(bad))).toEqual([]);
    }
    expect(epicTickets(null)).toEqual([]);
  });

  it("defaults a ticket whose fields are the wrong shape instead of dropping the row", () => {
    const tickets = epicTickets(withTickets({ tickets: [{ external_key: "T1", title: 42, depends_on: "T0" }] }));
    expect(tickets).toHaveLength(1);
    expect(tickets[0]).toMatchObject({ external_key: "T1", title: "", depends_on: [] });
  });

  it("shows only what a replay would still create", () => {
    const art = withTickets({
      tickets: [
        { external_key: "T1", title: "Add it", description: "", depends_on: [] },
        { external_key: "T2", title: "Test it", description: "", depends_on: ["T1"] },
      ],
      applied: { T1: "issue-1" },
    });
    expect(epicAppliedKeys(art)).toEqual(["T1"]);
    expect(epicRemainingTickets(art).map((t) => t.external_key)).toEqual(["T2"]);
  });

  it("ignores an applied map that is not one", () => {
    for (const bad of [{ applied: "nope" }, { applied: [] }, { applied: { T1: 1 } }, {}]) {
      expect(epicAppliedKeys(withTickets(bad))).toEqual([]);
    }
  });
});

describe("epic client", () => {
  const client = () => new ApiClient("https://api.example.test");

  it("gets the pipeline and falls back to an empty one", async () => {
    stubFetch({ steps: { prd: { kind: "prd", latest: artifact(), approved: null, generating: false } }, epic_issue_id: "i1", next_kind: "prd" });
    await expect(client().getProjectEpic("p1")).resolves.toMatchObject({ epic_issue_id: "i1", next_kind: "prd" });

    stubFetch("nope");
    await expect(client().getProjectEpic("p1")).resolves.toEqual(EMPTY_EPIC);
  });

  it("generates, edits, approves and applies, tolerating malformed answers", async () => {
    stubFetch({ task_id: "t1", kind: "prd" }, 202);
    await expect(client().generateProjectEpicStep("p1", "prd")).resolves.toEqual({ task_id: "t1", kind: "prd" });
    stubFetch(["not an object"], 202);
    await expect(client().generateProjectEpicStep("p1", "prd")).resolves.toEqual({ task_id: "", kind: "prd" });

    stubFetch({ artifact: artifact({ version: 2 }), reopened_steps: ["tech_plan"], epic_issue_id: "i1", superseded_previous: true });
    await expect(client().putProjectEpicStep("p1", "prd", "# PRD")).resolves.toMatchObject({ reopened_steps: ["tech_plan"] });
    stubFetch(7);
    await expect(client().putProjectEpicStep("p1", "prd", "# PRD")).resolves.toEqual({ reopened_steps: [] });

    stubFetch({ created: [{ id: "i2" }], existing: ["T1"], dependencies: 1, epic_issue_id: "i1" });
    await expect(client().applyProjectEpicTickets("p1")).resolves.toMatchObject({ existing: ["T1"], dependencies: 1 });
    stubFetch("nope");
    await expect(client().applyProjectEpicTickets("p1")).resolves.toEqual({ created: [], existing: [], dependencies: 0, epic_issue_id: "" });
  });
});
