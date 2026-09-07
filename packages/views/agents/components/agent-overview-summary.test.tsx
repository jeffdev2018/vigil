// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { AgentOverviewSummary } from "./agent-overview-summary";

// The Skills row is the only interactive spot on this otherwise read-only
// aside, and it exists because `SkillAttach` sat unmounted after the workbench
// rebuild moved this row out of the old inspector and dropped its chip. The
// chip's own module had no test; the row had no test that it was there. This
// file asserts the row renders it, and that `canEdit` still gates it.

const workspaceSkills = vi.hoisted(() => ({ current: [] as Array<{ id: string; name: string }> }));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: workspaceSkills.current }),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/workspace/queries", () => ({
  skillListOptions: () => ({ queryKey: ["skills"] }),
}));
// Heavy children of the aside: each owns its own queries and is covered by its
// own suite.
vi.mock("./tabs/activity-tab", () => ({ AgentPerformanceSummary: () => null }));
vi.mock("./agent-scorecard-section", () => ({ AgentScorecardSection: () => null }));
vi.mock("./agent-competency-section", () => ({ AgentCompetencySection: () => null }));
vi.mock("./agent-routing-check", () => ({ AgentRoutingCheck: () => null }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("./skill-add-dialog", () => ({ SkillAddDialog: () => null }));

const agent = {
  id: "agent-1",
  name: "Scout",
  model: "claude",
  max_concurrent_tasks: 2,
  visibility: "workspace",
  skills: [{ id: "s-1", name: "Repo triage" }],
} as unknown as Agent;

const runtime = { id: "r-1", status: "online", provider: "claude" } as unknown as AgentRuntime;

function renderSummary(canEdit: boolean) {
  renderWithI18n(
    <AgentOverviewSummary agent={agent} runtime={runtime} owner={null} canEdit={canEdit} />,
  );
}

describe("AgentOverviewSummary skills row", () => {
  it("renders the attach chip next to the skill chips when the viewer can edit", () => {
    workspaceSkills.current = [
      { id: "s-1", name: "Repo triage" },
      { id: "s-2", name: "Release notes" },
    ];
    renderSummary(true);
    expect(screen.getByText("Repo triage")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Attach a workspace skill" })).toBeTruthy();
  });

  it("hides the chip for a viewer who cannot edit", () => {
    workspaceSkills.current = [{ id: "s-2", name: "Release notes" }];
    renderSummary(false);
    expect(screen.queryByRole("button", { name: "Attach a workspace skill" })).toBeNull();
  });

  it("hides the chip when every workspace skill is already attached", () => {
    workspaceSkills.current = [{ id: "s-1", name: "Repo triage" }];
    renderSummary(true);
    expect(screen.queryByRole("button", { name: "Attach a workspace skill" })).toBeNull();
  });
});
