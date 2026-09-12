// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Project } from "../types/project";
import { buildRoadmapTimeline, dateToOffset } from "./timeline";

const TODAY = new Date(Date.UTC(2026, 8, 11));
const MS_PER_DAY = 24 * 60 * 60 * 1000;

describe("buildRoadmapTimeline", () => {
  it("separates projects with no dates into unscheduled", () => {
    const t = buildRoadmapTimeline(
      [project({ id: "a", start_date: "2026-09-01", due_date: "2026-09-10" }), project({ id: "b" })],
      TODAY,
    );
    expect(t.rows.map((r) => r.project.id)).toEqual(["a"]);
    expect(t.unscheduled.map((p) => p.id)).toEqual(["b"]);
  });

  it("sorts rows by startDate then dueDate", () => {
    const t = buildRoadmapTimeline(
      [
        project({ id: "c", start_date: "2026-09-05", due_date: "2026-09-20" }),
        project({ id: "a", start_date: "2026-09-01", due_date: "2026-09-15" }),
        project({ id: "b", start_date: "2026-09-01", due_date: "2026-09-02" }),
      ],
      TODAY,
    );
    expect(t.rows.map((r) => r.project.id)).toEqual(["b", "a", "c"]);
  });

  it("gives a single-date project a one-day marker", () => {
    const onlyStart = buildRoadmapTimeline([project({ start_date: "2026-09-01" })], TODAY);
    expect(onlyStart.rows[0]?.startDate).toEqual(onlyStart.rows[0]?.dueDate);
    const onlyDue = buildRoadmapTimeline([project({ due_date: "2026-09-01" })], TODAY);
    expect(onlyDue.rows[0]?.startDate).toEqual(onlyDue.rows[0]?.dueDate);
  });

  it("normalizes an inverted start/due pair", () => {
    const t = buildRoadmapTimeline(
      [project({ start_date: "2026-09-10", due_date: "2026-09-01" })],
      TODAY,
    );
    expect(t.rows[0]?.startDate.toISOString().slice(0, 10)).toBe("2026-09-01");
    expect(t.rows[0]?.dueDate.toISOString().slice(0, 10)).toBe("2026-09-10");
  });

  it("always includes today in the range with a one-day margin on both sides", () => {
    const t = buildRoadmapTimeline(
      [project({ start_date: "2026-01-01", due_date: "2026-01-10" })],
      TODAY,
    );
    expect(t.rangeStart.getTime()).toBeLessThanOrEqual(TODAY.getTime() - MS_PER_DAY);
    expect(t.rangeEnd.getTime()).toBeGreaterThanOrEqual(TODAY.getTime() + MS_PER_DAY);
    expect(t.rangeStart.toISOString().slice(0, 10)).toBe("2025-12-31");
  });

  it("keeps a range around today even with no dated projects", () => {
    const t = buildRoadmapTimeline([project({ id: "u" })], TODAY);
    expect(t.rows).toEqual([]);
    expect(t.rangeStart.getTime()).toBe(TODAY.getTime() - MS_PER_DAY);
    expect(t.rangeEnd.getTime()).toBe(TODAY.getTime() + MS_PER_DAY);
  });

  it("computes progress as the done ratio, 0 with no issues, clamped to 1", () => {
    const t = buildRoadmapTimeline(
      [
        project({ id: "empty", start_date: "2026-09-01", issue_count: 0, done_count: 0 }),
        project({ id: "half", start_date: "2026-09-01", issue_count: 4, done_count: 2 }),
        project({ id: "full", start_date: "2026-09-01", issue_count: 2, done_count: 2 }),
        project({ id: "over", start_date: "2026-09-01", issue_count: 2, done_count: 5 }),
      ],
      TODAY,
    );
    const progress = new Map(t.rows.map((r) => [r.project.id, r.progress]));
    expect(progress.get("empty")).toBe(0);
    expect(progress.get("half")).toBe(0.5);
    expect(progress.get("full")).toBe(1);
    expect(progress.get("over")).toBe(1);
  });
});

describe("dateToOffset", () => {
  const start = new Date(Date.UTC(2026, 8, 1));
  const end = new Date(Date.UTC(2026, 8, 11));

  it("maps range bounds to 0 and 1 and the middle to 0.5", () => {
    expect(dateToOffset(start, start, end)).toBe(0);
    expect(dateToOffset(end, start, end)).toBe(1);
    expect(dateToOffset(new Date(Date.UTC(2026, 8, 6)), start, end)).toBe(0.5);
  });

  it("clamps dates outside the range", () => {
    expect(dateToOffset(new Date(Date.UTC(2026, 0, 1)), start, end)).toBe(0);
    expect(dateToOffset(new Date(Date.UTC(2027, 0, 1)), start, end)).toBe(1);
  });

  it("returns 0 for a degenerate range", () => {
    expect(dateToOffset(start, start, start)).toBe(0);
  });
});

const project = (over: Partial<Project>): Project => ({
  id: "p",
  workspace_id: "ws",
  title: "Project",
  description: null,
  icon: null,
  status: "in_progress",
  priority: "none",
  lead_type: null,
  lead_id: null,
  start_date: null,
  due_date: null,
  created_at: "",
  updated_at: "",
  issue_count: 0,
  done_count: 0,
  resource_count: 0,
  ...over,
});
