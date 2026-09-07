// @vitest-environment node

import { describe, expect, it } from "vitest";
import {
  BOARD_IN_REVIEW_MULTICA_SIDE_EFFECTS,
  HARD_AUTHORIZATION_GATES,
  SOFT_AUTHORIZATION_SIGNALS,
  authorizationKind,
  isHardAuthorizationGate,
  isSoftAuthorizationSignal,
} from "./authorization-frontiers";

describe("authorization frontiers", () => {
  it("classifies board and delivery signals as soft — never merge/deploy auth", () => {
    for (const id of SOFT_AUTHORIZATION_SIGNALS) {
      expect(authorizationKind(id)).toBe("soft_signal");
      expect(isSoftAuthorizationSignal(id)).toBe(true);
      expect(isHardAuthorizationGate(id)).toBe(false);
    }
    expect(isSoftAuthorizationSignal("board_status_in_review")).toBe(true);
    expect(isSoftAuthorizationSignal("delivery_acceptance")).toBe(true);
    expect(isSoftAuthorizationSignal("coding_tool_approval_prompts")).toBe(true);
  });

  it("classifies Multica-enforced frontiers as hard gates", () => {
    for (const id of HARD_AUTHORIZATION_GATES) {
      expect(authorizationKind(id)).toBe("hard_gate");
      expect(isHardAuthorizationGate(id)).toBe(true);
      expect(isSoftAuthorizationSignal(id)).toBe(false);
    }
    expect(isHardAuthorizationGate("private_agent_invoke")).toBe(true);
    expect(isHardAuthorizationGate("run_scoped_multica_token")).toBe(true);
    expect(isHardAuthorizationGate("delivery_review_cas")).toBe(true);
  });

  it("refuses unknown ids so callers cannot invent gates by typo", () => {
    expect(authorizationKind("merge_pull_request")).toBe("unknown");
    expect(isSoftAuthorizationSignal("merge_pull_request")).toBe(false);
    expect(isHardAuthorizationGate("merge_pull_request")).toBe(false);
    expect(authorizationKind("deploy_production")).toBe("unknown");
  });

  it("documents that in_review Multica side effects stay inside Multica", () => {
    expect(BOARD_IN_REVIEW_MULTICA_SIDE_EFFECTS).toContain(
      "finalize_autopilot_run",
    );
    expect(BOARD_IN_REVIEW_MULTICA_SIDE_EFFECTS).toContain(
      "archive_run_failure_notifications",
    );
    expect(BOARD_IN_REVIEW_MULTICA_SIDE_EFFECTS).not.toContain("merge_pull_request");
    expect(BOARD_IN_REVIEW_MULTICA_SIDE_EFFECTS).not.toContain("deploy_production");
  });

  it("keeps soft and hard registries disjoint", () => {
    const overlap = SOFT_AUTHORIZATION_SIGNALS.filter((id) =>
      (HARD_AUTHORIZATION_GATES as readonly string[]).includes(id),
    );
    expect(overlap).toEqual([]);
  });
});
