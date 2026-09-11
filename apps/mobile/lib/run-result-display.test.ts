import { describe, expect, it } from "vitest";
import { runResultDisplay } from "./run-result-display";

// Audit UX (sept. 2026): "Reported result" printed the run's raw JSON on
// mobile too. Parsing matrix: packages/core/issues/run-result.test.ts.
describe("runResultDisplay", () => {
  it("keeps the output, the pull request and the goal verdict, never machine state", () => {
    const view = runResultDisplay({
      output: "Done", pr_url: "https://github.com/acme/app/pull/7", work_dir: "/Users/jeff/ws/x", session_id: "sess-1",
      goal_loop: { continuation: 1, signature: "deadbeef", no_progress: 0, outcome: "stopped:needs_user_input", blocker: "needs_user_input", reason: "Market missing", next_step: "Name it" },
    });
    expect(view?.output).toBe("Done");
    expect(view?.prUrl).toBe("https://github.com/acme/app/pull/7");
    expect(view?.rows).toEqual([
      { label: "Goal", value: "Stopped — needs your input" },
      { label: "Blocker", value: "Needs your input" },
      { label: "Reason", value: "Market missing" },
      { label: "Next step", value: "Name it" },
    ]);
    expect(view?.technical).toContain("continuation");
    expect(JSON.stringify(view)).not.toMatch(/Users\/jeff|sess-1|deadbeef/);
  });

  it("is null when nothing was reported", () => {
    expect(runResultDisplay(null)).toBeNull();
  });
});
