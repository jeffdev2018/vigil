// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseRunResult } from "./run-result";

describe("parseRunResult", () => {
  it("presents the output, pull request and goal verdict, and hides machine state", () => {
    const view = parseRunResult({
      output: "## Done\nAnalysis attached.",
      pr_url: "https://github.com/acme/app/pull/7",
      work_dir: "/Users/jeff/multica_workspaces/one/abc",
      durable_work_dir: "/Users/jeff/code/app",
      session_id: "sess-123",
      branch_name: "agent/one-96",
      goal_loop: {
        continuation: 2, signature: "deadbeef", no_progress: 0, outcome: "stopped:needs_user_input",
        blocker: "needs_user_input", reason: "The market name is missing.", evidence: "Searched three sources.", next_step: "Name the market.",
      },
    });
    expect(view).toEqual({
      output: "## Done\nAnalysis attached.",
      prUrl: "https://github.com/acme/app/pull/7",
      goal: {
        outcome: "stopped:needs_user_input", blocker: "needs_user_input", reason: "The market name is missing.",
        nextStep: "Name the market.", evidence: ["Searched three sources."],
      },
      technical: { branch_name: "agent/one-96", goal_loop: { continuation: 2, no_progress: 0 } },
    });
    expect(JSON.stringify(view)).not.toMatch(/Users\/jeff|sess-123|deadbeef/);
  });

  it("reads older summary payloads and plain strings, and returns null for nothing", () => {
    expect(parseRunResult({ summary: "Fixed it" })?.output).toBe("Fixed it");
    expect(parseRunResult("plain")?.output).toBe("plain");
    expect(parseRunResult(null)).toBeNull();
    expect(parseRunResult({ work_dir: "/tmp/x", session_id: "s", output: "" })).toBeNull();
  });

  it("drops a non-http pull request link and survives malformed fields", () => {
    const view = parseRunResult({ pr_url: "javascript:alert(1)", output: 42, goal_loop: "nope", evidence: [1] });
    expect(view?.prUrl).toBeNull();
    expect(view?.output).toBeNull();
    expect(view?.goal).toBeNull();
    expect(view?.technical).toEqual({ evidence: [1] });
  });
});
