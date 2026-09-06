// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  CODE_HEALTH_DEFAULT_SETTINGS,
  CodeHealthScanListSchema,
  CodeHealthSettingsSchema,
  hasRunningScan,
  openedFindings,
  scanTone,
  type CodeHealthScan,
} from "./schemas";

// Code health autopilot (K22). The API boundary must survive a backend that
// drifted: installed desktop builds talk to newer servers, so an unknown
// status or a missing field parses rather than throwing.

function scan(overrides: Partial<CodeHealthScan> = {}): CodeHealthScan {
  return {
    id: "s1",
    workspace_id: "w1",
    project_id: null,
    agent_id: "a1",
    task_id: "t1",
    status: "completed",
    findings: [],
    issues_created: 0,
    error: "",
    created_at: "2026-01-01T00:00:00Z",
    completed_at: "2026-01-01T00:10:00Z",
    ...overrides,
  };
}

describe("CodeHealthSettingsSchema", () => {
  it("fills in every field a sparse response omits", () => {
    const parsed = CodeHealthSettingsSchema.parse({});
    expect(parsed.enabled).toBe(false);
    expect(parsed.cron).toBe(CODE_HEALTH_DEFAULT_SETTINGS.cron);
    expect(parsed.max_issues_per_scan).toBe(5);
    expect(parsed.min_confidence).toBe(70);
  });

  it("keeps a field of the wrong type from failing the whole parse", () => {
    const parsed = CodeHealthSettingsSchema.parse({
      enabled: "yes",
      max_issues_per_scan: null,
      agent_id: "agent-1",
    });
    expect(parsed.enabled).toBe(false);
    expect(parsed.max_issues_per_scan).toBe(5);
    expect(parsed.agent_id).toBe("agent-1");
  });
});

describe("CodeHealthScanListSchema", () => {
  it("reads a malformed list as empty rather than throwing", () => {
    expect(CodeHealthScanListSchema.parse({ scans: "boom" }).scans).toEqual([]);
    expect(CodeHealthScanListSchema.parse({}).scans).toEqual([]);
  });

  it("keeps a status a newer server invented", () => {
    const parsed = CodeHealthScanListSchema.parse({
      scans: [{ id: "s1", status: "quarantined", findings: [{ title: "x" }] }],
    });
    expect(parsed.scans[0]?.status).toBe("quarantined");
    expect(parsed.scans[0]?.findings[0]?.confidence).toBe(0);
  });
});

describe("hasRunningScan", () => {
  it("is false for an empty or undefined history", () => {
    expect(hasRunningScan(undefined)).toBe(false);
    expect(hasRunningScan([])).toBe(false);
  });

  it("is true as soon as one scan is still running", () => {
    expect(hasRunningScan([scan(), scan({ id: "s2", status: "running" })])).toBe(true);
  });
});

describe("openedFindings", () => {
  it("keeps the findings that produced an issue, budget-refused ones included", () => {
    const opened = openedFindings(
      scan({
        findings: [
          { kind: "tests", title: "kept", summary: "", paths: [], confidence: 90, effort: "M", evidence: "", issue_id: "i1", skipped: "" },
          { kind: "debt", title: "skipped", summary: "", paths: [], confidence: 10, effort: "S", evidence: "", issue_id: "", skipped: "low_confidence" },
          // Refused by budget admission: the issue exists, the run does not.
          { kind: "debt", title: "refused", summary: "", paths: [], confidence: 90, effort: "S", evidence: "", issue_id: "i2", skipped: "budget" },
        ],
      }),
    );
    // "refused" has an issue but no run: the issue must stay reachable.
    expect(opened.map((f) => f.title)).toEqual(["kept", "refused"]);
  });
});

describe("scanTone", () => {
  it("bands the known statuses and falls back for anything else", () => {
    expect(scanTone("completed")).toBe("success");
    expect(scanTone("running")).toBe("warning");
    expect(scanTone("failed")).toBe("destructive");
    // "empty" is not a failure: a clean repository is the normal outcome.
    expect(scanTone("empty")).toBe("muted");
    expect(scanTone("quarantined")).toBe("muted");
  });
});
