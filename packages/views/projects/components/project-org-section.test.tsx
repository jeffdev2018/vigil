// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import type { OrgStructure, OrgTemplate } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";

const state = vi.hoisted(() => ({ structure: null as OrgStructure | null, created: [] as unknown[] }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ org: () => "/acme/org" }) }));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: (o: { queryKey?: readonly unknown[] }) =>
    o.queryKey?.[0] === "org-resolve" ? { data: state.structure } : { data: [TEMPLATE], isPending: false },
}));
vi.mock("@multica/core/org", () => ({
  orgResolveOptions: () => ({ queryKey: ["org-resolve"] }),
  orgTemplatesOptions: () => ({ queryKey: ["org-templates"] }),
  useCreateOrgStructure: () => ({ isPending: false, mutate: (v: unknown) => state.created.push(v) }),
}));

import { ProjectOrgSection } from "./project-org-section";

const definition = { units: [], edges: [], rules: [], committees: [], market: { price_cap_usd_ticks: 0, offers_per_agent_per_day: 0, min_offers: 0 } };
const TEMPLATE: OrgTemplate = { model: "taskforce", name: "Task force", pattern: "temporary", description: "Ends on a date.", coordination_runs_per_issue: 1, definition };

const structure = (over: Partial<OrgStructure>): OrgStructure => ({
  id: "s", workspace_id: "ws-1", project_id: null, model: "hierarchy", name: "Default org", status: "active", revision: 1, revision_id: null,
  definition, owner_id: null, dissolve_at: null, end_condition: "", budget_usd_ticks: 0, eval_attestation: "", paused_reason: "", dissolved_at: null,
  paused_units: [], created_by: null, created_at: "", updated_at: "", ...over,
});

const adapter: NavigationAdapter = { push: vi.fn(), replace: vi.fn(), back: vi.fn(), getCurrentPath: () => "/acme/projects/p1", switchWorkspace: vi.fn() } as unknown as NavigationAdapter;
const render = () => renderWithI18n(<NavigationProvider value={adapter}><ProjectOrgSection projectId="p1" /></NavigationProvider>);

beforeEach(() => {
  state.structure = null;
  state.created = [];
});

describe("ProjectOrgSection", () => {
  it("shows the inherited workspace default and offers to choose a model", () => {
    state.structure = structure({});
    render();
    const row = screen.getByTestId("project-org-structure").textContent;
    expect(row).toContain("Default org");
    expect(row).toContain("Hierarchy");
    expect(row).toContain("Inherited from the workspace default");
    expect(screen.getByRole("button", { name: "Choose a model" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open org" })).toHaveAttribute("href", "/acme/org");
  });

  it("shows the project's own structure without the inherited hint or the chooser", () => {
    state.structure = structure({ project_id: "p1", name: "Apollo org", model: "squads", status: "draft" });
    render();
    const row = screen.getByTestId("project-org-structure").textContent;
    expect(row).toContain("Apollo org");
    expect(row).toContain("Autonomous squads");
    expect(row).not.toContain("Inherited");
    expect(screen.queryByRole("button", { name: "Choose a model" })).toBeNull();
  });

  it("drafts a structure for the project from the picked template", async () => {
    render();
    expect(screen.getByText("No structure applies to this project.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Choose a model" }));
    fireEvent.click(await screen.findByTestId("org-template"));
    expect(state.created[0]).toEqual({ project_id: "p1", model: "taskforce", name: "Task force", definition });
  });
});
