// @vitest-environment node
import { describe, expect, it } from "vitest";
import { IssueRecurrenceResponseSchema } from "@multica/core/api/schemas";
import type { IssueRecurrenceResponse } from "@multica/core/types";
import { parseWithFallback as parse } from "@/lib/parse-response";

/** Exactly what `api.getIssueRecurrence` does; the label only feeds the
 *  warn line. `null` is the fallback because "no rule" is a real answer. */
function parseRecurrence(data: unknown): IssueRecurrenceResponse | null {
  return parse<IssueRecurrenceResponse | null>(
    data,
    IssueRecurrenceResponseSchema,
    null,
    { endpoint: "test" },
  );
}

/**
 * Mobile's client-side parsing of GET/PUT /api/issues/{id}/recurrence.
 * Fixtures are hand-written against `server/internal/handler/
 * issue_recurrence.go`; the point of each malformed case is that a drifted or
 * truncated server response lands on a renderable value instead of taking the
 * issue screen down.
 *
 * The fallback is `null` on purpose — "no rule" is a real answer here (the
 * endpoint 404s for it), so an unreadable body degrades to the same nothing
 * the 404 produces rather than to an invented rule the section would render
 * as "this issue recurs at «»".
 */
const DOCUMENTED = {
  recurrence: {
    id: "r1",
    issue_id: "i-source",
    cron_expression: "0 9 * * 1",
    timezone: "Europe/Paris",
    mode: "schedule",
    enabled: true,
    next_run_at: "2026-09-14T07:00:00Z",
    last_occurrence_id: "i-12",
    occurrence_count: 3,
    created_by_type: "member",
    created_by_id: "m1",
    created_at: "2026-09-01T09:00:00Z",
    updated_at: "2026-09-08T09:00:00Z",
  },
  source: { id: "i-source", identifier: "ONE-12", title: "Weekly review" },
  occurrences: [
    {
      id: "i-14",
      identifier: "ONE-14",
      title: "Weekly review",
      status: "todo",
      created_at: "2026-09-08T07:00:00Z",
      due_date: "2026-09-10",
    },
    {
      id: "i-source",
      identifier: "ONE-12",
      title: "Weekly review",
      status: "done",
      created_at: "2026-09-01T09:00:00Z",
      due_date: null,
    },
  ],
  next_runs: [
    "2026-09-14T07:00:00Z",
    "2026-09-21T07:00:00Z",
    "2026-09-28T07:00:00Z",
  ],
};

describe("IssueRecurrenceResponseSchema", () => {
  it("parses the documented shape", () => {
    const parsed = parseRecurrence(DOCUMENTED);
    expect(parsed?.recurrence.cron_expression).toBe("0 9 * * 1");
    expect(parsed?.recurrence.mode).toBe("schedule");
    expect(parsed?.recurrence.enabled).toBe(true);
    expect(parsed?.source.identifier).toBe("ONE-12");
    expect(parsed?.occurrences).toHaveLength(2);
    expect(parsed?.next_runs).toHaveLength(3);
  });

  it("keeps an on_close rule with no cron and no next runs", () => {
    const parsed = parseRecurrence({
        ...DOCUMENTED,
        recurrence: {
          ...DOCUMENTED.recurrence,
          cron_expression: "",
          mode: "on_close",
          next_run_at: null,
        },
        next_runs: [],
      });
    expect(parsed?.recurrence.mode).toBe("on_close");
    expect(parsed?.recurrence.next_run_at).toBeNull();
    expect(parsed?.next_runs).toEqual([]);
  });

  it("keeps a mode the client has never heard of instead of dropping the rule", () => {
    const parsed = parseRecurrence({
        ...DOCUMENTED,
        recurrence: { ...DOCUMENTED.recurrence, mode: "on_first_comment" },
      });
    expect(parsed?.recurrence.mode).toBe("on_first_comment");
  });

  it("survives a source whose title the server could not resolve", () => {
    // `issueRecurrencePayload` only sets `title` when the source issue loads;
    // a soft-deleted source therefore answers with id + identifier only.
    const parsed = parseRecurrence({ ...DOCUMENTED, source: { id: "i-source", identifier: "ONE-12" } });
    expect(parsed?.source.title).toBe("");
    expect(parsed?.source.identifier).toBe("ONE-12");
  });

  it("degrades a malformed occurrence list to empty, keeping the rule", () => {
    const parsed = parseRecurrence({ ...DOCUMENTED, occurrences: "soon" });
    expect(parsed?.recurrence.id).toBe("r1");
    expect(parsed?.occurrences).toEqual([]);
  });

  it("degrades malformed next_runs to empty, keeping the rule", () => {
    const parsed = parseRecurrence({ ...DOCUMENTED, next_runs: { a: 1 } });
    expect(parsed?.recurrence.id).toBe("r1");
    expect(parsed?.next_runs).toEqual([]);
  });

  it("defaults a missing occurrence_count rather than rendering NaN", () => {
    const parsed = parseRecurrence({
        ...DOCUMENTED,
        recurrence: { ...DOCUMENTED.recurrence, occurrence_count: "three" },
      });
    expect(parsed?.recurrence.occurrence_count).toBe(0);
  });

  it("passes unknown server fields through untouched", () => {
    const parsed = parseRecurrence({
        ...DOCUMENTED,
        recurrence: { ...DOCUMENTED.recurrence, skip_weekends: true },
      });
    expect(parsed?.recurrence.id).toBe("r1");
  });

  it.each([
    ["a rule with no id", { ...DOCUMENTED, recurrence: { mode: "schedule" } }],
    ["no recurrence at all", { source: DOCUMENTED.source }],
    ["an error envelope", { error: "this issue does not recur" }],
    ["a bare string", "nope"],
    ["null", null],
  ])("falls back to null for %s", (_label, body) => {
    expect(parseRecurrence(body)).toBeNull();
  });
});
