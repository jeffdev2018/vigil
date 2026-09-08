// @vitest-environment node
import { describe, expect, it } from "vitest";
import { routingCheckSummary, type RoutingCheck } from "./routing-check";

// Canonical matrix for the routing-check status line (JEF-275). The component
// suite only mounts the happy path and points here.

const check = (problems: RoutingCheck["problems"]): RoutingCheck => ({
  agent_id: "a1",
  ok: problems.length === 0,
  fatal: problems.some((p) => p.fatal),
  problems,
});

describe("routingCheckSummary", () => {
  it("says nothing before the check has answered", () => {
    expect(routingCheckSummary(undefined)).toBeNull();
  });

  it("reports a clean agent with no message to render", () => {
    expect(routingCheckSummary(check([]))).toEqual({ tone: "ok", message: "", extra: 0 });
  });

  it("shows a warning for a problem waiting resolves", () => {
    const s = routingCheckSummary(check([{ code: "runtime_offline_no_fallback", message: "waiting", fatal: false }]));
    expect(s).toEqual({ tone: "warning", message: "waiting", extra: 0 });
  });

  it("promotes the fatal problem over the warnings listed before it", () => {
    const s = routingCheckSummary(
      check([
        { code: "runtime_offline_no_fallback", message: "waiting", fatal: false },
        { code: "agent_archived", message: "archived", fatal: true },
      ]),
    );
    // The fatal one is what the reader must act on, whatever its position.
    expect(s).toEqual({ tone: "error", message: "archived", extra: 1 });
  });

  it("collapses several warnings to the first plus a count", () => {
    const s = routingCheckSummary(
      check([
        { code: "runtime_offline_no_fallback", message: "waiting", fatal: false },
        { code: "model_key_missing", message: "no key", fatal: false },
      ]),
    );
    expect(s).toEqual({ tone: "warning", message: "waiting", extra: 1 });
  });
});
