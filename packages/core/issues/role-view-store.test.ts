// @vitest-environment node
import { describe, expect, it } from "vitest";
import { isIssueRoleView, useIssueRoleViewStore } from "./role-view-store";

describe("issue role view store", () => {
  it("defaults to the full page, accepts the three presets and persists only the view", () => {
    expect(useIssueRoleViewStore.getState().view).toBe("full");
    useIssueRoleViewStore.getState().setView("cto");
    expect(useIssueRoleViewStore.getState().view).toBe("cto");
    useIssueRoleViewStore.getState().setView("nope" as never);
    expect(useIssueRoleViewStore.getState().view).toBe("full");
    const opts = useIssueRoleViewStore.persist.getOptions();
    expect(opts.name).toBe("multica_issue_role_view");
    expect(opts.partialize?.(useIssueRoleViewStore.getState())).toEqual({ view: "full" });
    expect(opts.merge?.({ view: "pm" }, useIssueRoleViewStore.getState()).view).toBe("pm");
    expect(opts.merge?.({ view: 42 }, useIssueRoleViewStore.getState()).view).toBe("full");
    expect(isIssueRoleView("qa")).toBe(true);
  });
});
