// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  AutopilotDraftResponseSchema,
  AutopilotProposalResponseSchema,
  CalendarAgendaSchema,
  FollowupResponseSchema,
  IssueFollowupsResponseSchema,
  EMPTY_AUTOPILOT_DRAFT,
  EMPTY_AUTOPILOT_PROPOSAL,
  EMPTY_CALENDAR_AGENDA,
  EMPTY_FOLLOWUP,
  EMPTY_ISSUE_FOLLOWUPS,
} from "@multica/core/api/schemas";
import { parseWithFallback as parse } from "@/lib/parse-response";

/** Envelope fallbacks: core exports the payload shapes, api.ts wraps them. */
const EMPTY_CREATE_FOLLOWUP = { followup: EMPTY_FOLLOWUP };
const EMPTY_AUTOPILOT_DRAFT_ENVELOPE = { draft: EMPTY_AUTOPILOT_DRAFT };

/** The endpoint label only feeds the warn line; every case here names its own. */
function parseWithFallback<T>(data: unknown, schema: Parameters<typeof parse>[1], fallback: T): T {
  return parse(data, schema, fallback, { endpoint: "test" });
}

/**
 * Mobile's client-side parsing of the réveil-programmé endpoints
 * (JEF-373). Fixtures are hand-written against
 * `server/internal/handler/followups.go` and `autopilot_draft.go`; the point
 * of each malformed case is that a drifted or truncated server response
 * lands on a renderable value instead of taking the issue screen down.
 */
describe("IssueFollowupsResponseSchema", () => {
  it("parses the documented shape", () => {
    const parsed = parseWithFallback(
      {
        followups: [
          {
            id: "f1",
            issue_id: "i1",
            agent_id: "a1",
            agent_name: "Ada",
            fires_at: "2026-09-11T09:00:00Z",
            note: "Check the deploy",
            scheduled_by_type: "member",
            scheduled_by_id: "u1",
            created_at: "2026-09-10T09:00:00Z",
          },
        ],
        budget: { max_per_agent_per_day: 20, max_per_workspace_per_day: 200 },
      },
      IssueFollowupsResponseSchema,
      EMPTY_ISSUE_FOLLOWUPS,
    );
    expect(parsed.followups).toHaveLength(1);
    expect(parsed.followups[0]?.agent_name).toBe("Ada");
    expect(parsed.budget.max_per_agent_per_day).toBe(20);
  });

  it("keeps a null scheduled_by_id (an agent scheduled it)", () => {
    const parsed = parseWithFallback(
      {
        followups: [
          {
            id: "f1",
            issue_id: "i1",
            agent_id: "a1",
            agent_name: "Ada",
            fires_at: "2026-09-11T09:00:00Z",
            note: "",
            scheduled_by_type: "agent",
            scheduled_by_id: null,
            created_at: "2026-09-10T09:00:00Z",
          },
        ],
        budget: {},
      },
      IssueFollowupsResponseSchema,
      EMPTY_ISSUE_FOLLOWUPS,
    );
    expect(parsed.followups[0]?.scheduled_by_id).toBeNull();
    // A truncated budget block reads as 0 rather than taking the list down;
    // the sheet only quotes a ceiling it actually received.
    expect(parsed.budget.max_per_workspace_per_day).toBe(0);
  });

  it("falls back when followups is not an array", () => {
    const parsed = parseWithFallback(
      { followups: "soon", budget: null },
      IssueFollowupsResponseSchema,
      EMPTY_ISSUE_FOLLOWUPS,
    );
    expect(parsed.followups).toEqual([]);
    expect(parsed.budget.max_per_agent_per_day).toBe(0);
  });

  it("falls back on a non-object body", () => {
    expect(
      parseWithFallback(
        "gateway timeout",
        IssueFollowupsResponseSchema,
        EMPTY_ISSUE_FOLLOWUPS,
      ),
    ).toEqual(EMPTY_ISSUE_FOLLOWUPS);
  });
});

describe("FollowupResponseSchema", () => {
  it("unwraps the created follow-up", () => {
    const parsed = parseWithFallback(
      {
        followup: {
          id: "f1",
          issue_id: "i1",
          agent_id: "a1",
          agent_name: "Ada",
          fires_at: "2026-09-11T09:00:00Z",
          note: "n",
          scheduled_by_type: "member",
          scheduled_by_id: "u1",
          created_at: "2026-09-10T09:00:00Z",
        },
      },
      FollowupResponseSchema,
      EMPTY_CREATE_FOLLOWUP,
    );
    expect(parsed.followup.id).toBe("f1");
  });

  it("falls back when the envelope has no followup", () => {
    expect(
      parseWithFallback(
        { ok: true },
        FollowupResponseSchema,
        EMPTY_CREATE_FOLLOWUP,
      ),
    ).toEqual(EMPTY_CREATE_FOLLOWUP);
  });
});

describe("AutopilotDraftResponseSchema", () => {
  it("parses a draft with its next runs", () => {
    const parsed = parseWithFallback(
      {
        draft: {
          title: "Open tickets",
          cron_expression: "0 9 * * 1",
          timezone: "Europe/Paris",
          description: "List the open tickets",
          execution_mode: "create_issue",
          issue_title_template: "Open tickets {{date}}",
          reason: "Weekly on Monday morning",
          next_runs: ["2026-09-14T07:00:00Z"],
          model: "claude",
        },
      },
      AutopilotDraftResponseSchema,
      EMPTY_AUTOPILOT_DRAFT_ENVELOPE,
    );
    expect(parsed.draft.cron_expression).toBe("0 9 * * 1");
    expect(parsed.draft.next_runs).toEqual(["2026-09-14T07:00:00Z"]);
  });

  it("survives a draft whose next_runs drifted to a scalar", () => {
    const parsed = parseWithFallback(
      {
        draft: {
          title: "t",
          cron_expression: "0 9 * * 1",
          timezone: "UTC",
          description: "d",
          execution_mode: "run_only",
          reason: "",
          next_runs: 3,
        },
      },
      AutopilotDraftResponseSchema,
      EMPTY_AUTOPILOT_DRAFT_ENVELOPE,
    );
    expect(parsed.draft.next_runs).toEqual([]);
    expect(parsed.draft.title).toBe("t");
  });

  it("falls back when draft is missing entirely", () => {
    const parsed = parseWithFallback(
      { error: "no model" },
      AutopilotDraftResponseSchema,
      EMPTY_AUTOPILOT_DRAFT_ENVELOPE,
    );
    expect(parsed.draft.cron_expression).toBe("");
  });
});

describe("AutopilotProposalResponseSchema", () => {
  it("parses the paused autopilot and its decision id", () => {
    const parsed = parseWithFallback(
      {
        autopilot: {
          id: "ap1",
          workspace_id: "w1",
          title: "Open tickets",
          status: "paused",
          assignee_type: "agent",
          assignee_id: "a1",
          execution_mode: "run_only",
          created_by_type: "member",
          created_by_id: "u1",
          created_at: "2026-09-10T09:00:00Z",
          updated_at: "2026-09-10T09:00:00Z",
        },
        decision_id: "d1",
        next_runs: ["2026-09-14T07:00:00Z"],
      },
      AutopilotProposalResponseSchema,
      EMPTY_AUTOPILOT_PROPOSAL,
    );
    expect(parsed.autopilot.status).toBe("paused");
    expect(parsed.decision_id).toBe("d1");
  });

  it("accepts a null decision_id (proposed with no issue)", () => {
    const parsed = parseWithFallback(
      {
        autopilot: {
          id: "ap1",
          workspace_id: "w1",
          title: "t",
          status: "paused",
          assignee_id: "a1",
          execution_mode: "run_only",
          created_by_type: "member",
          created_by_id: "u1",
          created_at: "2026-09-10T09:00:00Z",
          updated_at: "2026-09-10T09:00:00Z",
        },
        decision_id: null,
        next_runs: [],
      },
      AutopilotProposalResponseSchema,
      EMPTY_AUTOPILOT_PROPOSAL,
    );
    expect(parsed.decision_id).toBeNull();
    expect(parsed.autopilot.id).toBe("ap1");
  });

  it("falls back whole when the autopilot body is truncated", () => {
    const parsed = parseWithFallback(
      { autopilot: { id: "ap1" }, decision_id: "d1", next_runs: [] },
      AutopilotProposalResponseSchema,
      EMPTY_AUTOPILOT_PROPOSAL,
    );
    expect(parsed).toEqual(EMPTY_AUTOPILOT_PROPOSAL);
  });

  it("falls back on a malformed body", () => {
    expect(
      parseWithFallback(
        [1, 2, 3],
        AutopilotProposalResponseSchema,
        EMPTY_AUTOPILOT_PROPOSAL,
      ),
    ).toEqual(EMPTY_AUTOPILOT_PROPOSAL);
  });
});

describe("CalendarAgendaSchema", () => {
  it("carries the agenda's wake-ups", () => {
    const parsed = parseWithFallback(
      {
        from: "2026-09-10T00:00:00Z",
        to: "2026-09-24T00:00:00Z",
        events: [],
        issues_due: [],
        cycles: [],
        meetings: [],
        followups: [
          {
            id: "f1",
            issue_id: "i1",
            identifier: "JEF-1",
            issue_title: "Ship it",
            agent_id: "a1",
            agent_name: "Ada",
            fires_at: "2026-09-11T09:00:00Z",
            note: "Check the deploy",
          },
        ],
      },
      CalendarAgendaSchema,
      EMPTY_CALENDAR_AGENDA,
    );
    expect(parsed.followups).toHaveLength(1);
    expect(parsed.followups[0]?.identifier).toBe("JEF-1");
  });

  it("reads an older server (no followups field) as no wake-ups", () => {
    const parsed = parseWithFallback(
      {
        from: "",
        to: "",
        events: [],
        issues_due: [],
        cycles: [],
        meetings: [],
      },
      CalendarAgendaSchema,
      EMPTY_CALENDAR_AGENDA,
    );
    expect(parsed.followups).toEqual([]);
  });

  it("drops a followups field that is not an array", () => {
    const parsed = parseWithFallback(
      {
        from: "",
        to: "",
        events: [],
        issues_due: [],
        cycles: [],
        meetings: [],
        followups: { id: "f1" },
      },
      CalendarAgendaSchema,
      EMPTY_CALENDAR_AGENDA,
    );
    expect(parsed.followups).toEqual([]);
  });
});
