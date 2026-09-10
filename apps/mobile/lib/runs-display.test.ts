import { describe, expect, it } from "vitest";
import {
  blockerLabel,
  formatRunCost,
  formatRunDuration,
  formatRunSilence,
  groupRunsBySection,
  isRunSilent,
  runSection,
} from "./runs-display";
import type { Run, RunBlocker } from "@/data/schemas";

function run(overrides: Partial<Run>): Pick<Run, "status" | "blocked_on" | "silence_ms"> & Partial<Run> {
  return { status: "queued", blocked_on: null, silence_ms: 0, ...overrides };
}

describe("runSection", () => {
  it("puts queued/deferred/paused with no blocker in the queued bucket", () => {
    expect(runSection(run({ status: "queued" }))).toBe("queued");
    expect(runSection(run({ status: "deferred" }))).toBe("queued");
    expect(runSection(run({ status: "paused" }))).toBe("queued");
  });

  it("puts dispatched/running with no blocker in the running bucket", () => {
    expect(runSection(run({ status: "dispatched" }))).toBe("running");
    expect(runSection(run({ status: "running" }))).toBe("running");
  });

  it("puts completed/failed/cancelled in the finished bucket", () => {
    expect(runSection(run({ status: "completed" }))).toBe("finished");
    expect(runSection(run({ status: "failed" }))).toBe("finished");
    expect(runSection(run({ status: "cancelled" }))).toBe("finished");
  });

  it("waiting_local_directory with no blocker is blocked (runStateOf maps it there)", () => {
    expect(runSection(run({ status: "waiting_local_directory" }))).toBe("blocked");
  });

  it("blocked_on wins over status — a running task with a gate is blocked, not running", () => {
    const blocker: RunBlocker = { kind: "gate", summary: "git push", since: null };
    expect(runSection(run({ status: "running", blocked_on: blocker }))).toBe("blocked");
  });
});

describe("groupRunsBySection", () => {
  it("buckets every run and keeps empty buckets as []", () => {
    const runs = [run({ status: "queued" }), run({ status: "running" }), run({ status: "completed" })];
    const grouped = groupRunsBySection(runs);
    expect(grouped.queued).toHaveLength(1);
    expect(grouped.running).toHaveLength(1);
    expect(grouped.finished).toHaveLength(1);
    expect(grouped.blocked).toEqual([]);
  });
});

describe("isRunSilent", () => {
  it("is false for a queued or finished run regardless of silence_ms", () => {
    expect(isRunSilent(run({ status: "queued", silence_ms: 999_999 }))).toBe(false);
    expect(isRunSilent(run({ status: "completed", silence_ms: 999_999 }))).toBe(false);
  });

  it("is false for a running run under the 90s threshold", () => {
    expect(isRunSilent(run({ status: "running", silence_ms: 89_000 }))).toBe(false);
  });

  it("is true for a running run over the 90s threshold", () => {
    expect(isRunSilent(run({ status: "running", silence_ms: 90_001 }))).toBe(true);
  });
});

describe("blockerLabel", () => {
  it("returns null for no blocker", () => {
    expect(blockerLabel(null)).toBeNull();
    expect(blockerLabel(undefined)).toBeNull();
  });

  it("prefixes a known kind with its label", () => {
    expect(blockerLabel({ kind: "gate", summary: "git push", since: null })).toBe("Approval: git push");
    expect(blockerLabel({ kind: "goal_question", summary: "Ship it?", since: null })).toBe("Question: Ship it?");
  });

  it("falls back to the raw kind for one this build doesn't recognise", () => {
    expect(blockerLabel({ kind: "future_kind", summary: "new blocker shape", since: null })).toBe(
      "future_kind: new blocker shape",
    );
  });

  it("drops the colon when there's no summary", () => {
    expect(blockerLabel({ kind: "paused", summary: "", since: null })).toBe("Paused");
  });
});

describe("formatRunDuration / formatRunSilence", () => {
  it("renders 0s for zero, negative or NaN", () => {
    expect(formatRunDuration(0)).toBe("0s");
    expect(formatRunDuration(-1)).toBe("0s");
    expect(formatRunDuration(NaN)).toBe("0s");
  });

  it("renders whole seconds under a minute", () => {
    expect(formatRunDuration(45_000)).toBe("45s");
  });

  it("renders minutes and seconds under an hour", () => {
    expect(formatRunDuration(134_000)).toBe("2m 14s");
    expect(formatRunDuration(120_000)).toBe("2m");
  });

  it("renders hours and minutes under a day", () => {
    expect(formatRunDuration(3_900_000)).toBe("1h 5m");
  });

  it("renders days and hours at or beyond 24h", () => {
    expect(formatRunDuration(90_000_000)).toBe("1d 1h");
    expect(formatRunSilence(172_800_000)).toBe("2d");
  });
});

describe("formatRunCost", () => {
  it("is the shared 1e-10-tick USD formatter", () => {
    expect(formatRunCost(100_000_000)).toBe("$0.01");
  });
});
