// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "../api/client";
import { EMPTY_AUTOPILOT_PROPOSAL } from "../api/schemas";

// Autopilots from a sentence: draft (no write) and propose (paused autopilot
// behind a Decision Card). Boundary parsing only — the dialog wiring lives in
// packages/views/autopilots/components/autopilot-draft-dialog.test.tsx.

function stubFetchJson(body: unknown, status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(typeof body === "string" ? body : JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

const client = () => new ApiClient("https://api.example.test");

const validDraft = {
  title: "Monday open tickets",
  cron_expression: "0 9 * * 1",
  timezone: "Europe/Paris",
  description: "List the open tickets and post them.",
  execution_mode: "create_issue",
  issue_title_template: "Open tickets {{date}}",
  reason: "Weekly on Monday at 09:00.",
  next_runs: ["2026-09-14T07:00:00Z", "2026-09-21T07:00:00Z", "2026-09-28T07:00:00Z"],
  model: "test-model",
};

describe("draftAutopilot", () => {
  it("parses a well-formed draft", async () => {
    stubFetchJson({ draft: validDraft });
    const draft = await client().draftAutopilot({ text: "every Monday at 9…" });
    expect(draft.cron_expression).toBe("0 9 * * 1");
    expect(draft.next_runs).toHaveLength(3);
  });

  it("fills defaults for fields an older server omits", async () => {
    stubFetchJson({ draft: { title: "Something" } });
    const draft = await client().draftAutopilot({ text: "x" });
    expect(draft.timezone).toBe("UTC");
    expect(draft.execution_mode).toBe("run_only");
    expect(draft.next_runs).toEqual([]);
  });

  it("degrades a malformed body to the empty draft instead of throwing", async () => {
    stubFetchJson({ draft: "not-an-object" });
    const draft = await client().draftAutopilot({ text: "x" });
    expect(draft.title).toBe("");
    expect(draft.cron_expression).toBe("");
  });

  it("keeps a malformed next_runs from costing the rest of the draft", async () => {
    stubFetchJson({ draft: { ...validDraft, next_runs: "soon" } });
    const draft = await client().draftAutopilot({ text: "x" });
    expect(draft.title).toBe("Monday open tickets");
    expect(draft.next_runs).toEqual([]);
  });

  it("surfaces 503 (no model configured) as an ApiError the dialog can catch", async () => {
    stubFetchJson({ error: "no model is configured to draft; write the schedule yourself" }, 503);
    await expect(client().draftAutopilot({ text: "x" })).rejects.toMatchObject({ status: 503 });
  });

  it("surfaces 502 (the model answered badly) as an ApiError", async () => {
    stubFetchJson({ error: "the model produced an invalid schedule" }, 502);
    await expect(client().draftAutopilot({ text: "x" })).rejects.toBeInstanceOf(ApiError);
  });
});

describe("proposeAutopilot", () => {
  const autopilot = {
    id: "ap-1",
    workspace_id: "ws-1",
    title: "Monday open tickets",
    description: "List them.",
    assignee_type: "agent",
    assignee_id: "a-1",
    status: "paused",
    execution_mode: "create_issue",
    issue_title_template: null,
    created_by_type: "member",
    created_by_id: "u-1",
    last_run_at: null,
    created_at: "2026-09-10T00:00:00Z",
    updated_at: "2026-09-10T00:00:00Z",
  };

  it("parses the proposal with its decision id and next runs", async () => {
    stubFetchJson({ autopilot, decision_id: "d-1", next_runs: ["2026-09-14T07:00:00Z"] }, 201);
    const res = await client().proposeAutopilot({ text: "x", assignee_id: "a-1" });
    expect(res.autopilot.status).toBe("paused");
    expect(res.decision_id).toBe("d-1");
    expect(res.next_runs).toEqual(["2026-09-14T07:00:00Z"]);
  });

  it("reads a proposal with no issue as one with no card", async () => {
    stubFetchJson({ autopilot, decision_id: null, next_runs: [] }, 201);
    const res = await client().proposeAutopilot({ text: "x", assignee_id: "a-1" });
    expect(res.decision_id).toBeNull();
  });

  it("degrades a malformed body to the empty proposal instead of throwing", async () => {
    stubFetchJson({ autopilot: "nope" }, 201);
    const res = await client().proposeAutopilot({ text: "x", assignee_id: "a-1" });
    expect(res).toEqual(EMPTY_AUTOPILOT_PROPOSAL);
  });

  it("sends activate and reads back an active autopilot with no card", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          autopilot: { ...autopilot, status: "active" },
          decision_id: null,
          next_runs: ["2026-09-14T07:00:00Z"],
        }),
        { status: 201, headers: { "Content-Type": "application/json" } },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);
    const res = await client().proposeAutopilot({ text: "x", assignee_id: "a-1", activate: true });
    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toMatchObject({ activate: true });
    expect(res.autopilot.status).toBe("active");
    expect(res.decision_id).toBeNull();
  });

  it("surfaces the 403 a run gets for asking to activate", async () => {
    stubFetchJson({ error: "only a member can activate; a run proposes and a person decides" }, 403);
    await expect(
      client().proposeAutopilot({ text: "x", assignee_id: "a-1", activate: true }),
    ).rejects.toMatchObject({ status: 403 });
  });

  it("surfaces 503 when no model can turn the sentence into a schedule", async () => {
    stubFetchJson({ error: "no model is configured to draft" }, 503);
    await expect(client().proposeAutopilot({ text: "x" })).rejects.toMatchObject({ status: 503 });
  });
});
