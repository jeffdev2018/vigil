// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import { fireEvent, screen, within } from "@testing-library/react";
import type { OrgDefinition, OrgHealth, OrgStructure, OrgTemplate } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Mermaid source and model labels: packages/core/org/queries.test.ts.

const state = vi.hoisted(() => ({
  structures: [] as OrgStructure[],
  templates: [] as OrgTemplate[],
  health: null as OrgHealth | null,
  created: [] as unknown[],
  updated: [] as unknown[],
  status: [] as unknown[],
  toastError: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/auth", () => ({ useAuthStore: (sel: (s: unknown) => unknown) => sel({ user: { id: "u-1" } }) }));
vi.mock("@multica/core/workspace/queries", () => ({ squadListOptions: () => ({ queryKey: ["squads"] }), agentListOptions: () => ({ queryKey: ["agents"] }), memberListOptions: () => ({ queryKey: ["members"] }) }));
vi.mock("@multica/core/projects/queries", () => ({ projectListOptions: () => ({ queryKey: ["projects"] }) }));
vi.mock("@multica/core/goals", () => ({ goalListOptions: () => ({ queryKey: ["goals"] }) }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ newAgent: () => "/test/agents/new" }) }));
vi.mock("../../navigation", () => ({ useNavigation: () => ({ push: vi.fn() }) }));
vi.mock("sonner", () => ({ toast: { error: state.toastError, success: vi.fn() } }));
vi.mock("../../settings/components/export-import-setting", () => ({ ExportImportSetting: () => null }));
vi.mock("./org-team-catalog", () => ({ OrgTeamCatalog: () => null }));
vi.mock("../../editor/mermaid-diagram", () => ({ MermaidDiagram: ({ chart }: { chart: string }) => <pre data-testid="mermaid">{chart}</pre> }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: (o: { queryKey?: readonly unknown[] }) => {
    const [key, id] = o.queryKey ?? [];
    if (key === "org-list") return { data: state.structures, isLoading: false, isPending: false };
    if (key === "org-detail") {
      const structure = state.structures.find((s) => s.id === id);
      return { data: structure ? { structure, revisions: [{ id: "r1", revision: 1, model: structure.model, status: "draft", note: "", changed_by: null, created_at: "2026-09-01T10:00:00Z" }] } : null, isPending: false };
    }
    if (key === "org-templates") return { data: state.templates, isPending: false };
    if (key === "org-health") return { data: state.health, isPending: false };
    if (key === "org-preflight") return { data: { model: "hierarchy", pattern: "manager → workers", coordination_runs_per_issue: 2, coordination_cost_usd_ticks_per_issue: 1_500_000, human_review_items_per_issue: 1, human_review_seconds_per_issue: 90, units: 2, units_without_owner: 0, agents: 3, activation_requirements: [] } };
    if (key === "agents") return { data: [{ id: "a-1", name: "Mika", trust_mode: "approval" }, { id: "a-2", name: "Nia", trust_mode: "approval" }, { id: "a-3", name: "Sol", trust_mode: "approval" }] };
    if (key === "members") return { data: [{ user_id: "u-1", name: "Ada", role: "owner" }], isLoading: false };
    if (key === "projects") return { data: [{ id: "p-1", title: "Apollo" }], isLoading: false };
    return { data: undefined, isLoading: false, isPending: true };
  },
}));
vi.mock("@multica/core/org", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/org")>()),
  orgListOptions: () => ({ queryKey: ["org-list"] }),
  orgDetailOptions: (_ws: string, id: string) => ({ queryKey: ["org-detail", id] }),
  orgTemplatesOptions: () => ({ queryKey: ["org-templates"] }),
  orgHealthOptions: (_ws: string, id: string) => ({ queryKey: ["org-health", id] }),
  orgPreflightOptions: (_ws: string, id: string) => ({ queryKey: ["org-preflight", id] }),
  useCreateOrgStructure: () => ({ isPending: false, mutate: (data: unknown, o: { onSuccess: (s: unknown) => void }) => { state.created.push(data); o.onSuccess(null); } }),
  useUpdateOrgStructure: () => ({ isPending: false, mutate: (v: unknown, o: { onSuccess: () => void }) => { state.updated.push(v); o.onSuccess(); } }),
  useSetOrgStructureStatus: () => ({ isPending: false, mutate: (v: unknown, o: { onSuccess: () => void }) => { state.status.push(v); o.onSuccess(); } }),
  useDeleteOrgStructure: () => ({ isPending: false, mutate: vi.fn() }),
}));

import { useOrgDraftStore, useOrgWizardDraftStore } from "@multica/core/org/draft-store";
import { OrgPage } from "./org-page";

const definition: OrgDefinition = {
  units: [
    { id: "lead", name: "Lead", owner_id: "u-1", excludes: ["external_effects"], autonomy: "approve_payload", allow: [], deny: [], escalation_quota_per_day: 3, members: [{ type: "member", id: "u-1" }], roles: [] },
    { id: "dev", name: "Dev", excludes: ["external_effects"], autonomy: "draft", allow: [], deny: [], escalation_quota_per_day: 3, members: [{ type: "agent", id: "a-1" }, { type: "agent", id: "a-2" }], roles: [] },
  ],
  edges: [{ from: "dev", to: "lead", kind: "reports_to" }],
  rules: [],
  committees: [],
  market: { price_cap_usd_ticks: 0, offers_per_agent_per_day: 0, min_offers: 0 },
};

const structure = (over: Partial<OrgStructure>): OrgStructure => ({
  id: "s", workspace_id: "ws-1", project_id: null, model: "hierarchy", name: "Default org", status: "draft", revision: 1, revision_id: "r1",
  definition, owner_id: "u-1", dissolve_at: null, end_condition: "", budget_usd_ticks: 0, eval_attestation: "", paused_reason: "", dissolved_at: null,
  paused_units: [], created_by: null, created_at: "2026-09-01T10:00:00Z", updated_at: "2026-09-01T10:00:00Z", ...over,
});


beforeEach(() => {
  useOrgDraftStore.getState().clearDraft();
  useOrgWizardDraftStore.getState().clearDraft();
  state.structures = [];
  state.templates = [];
  state.health = null;
  state.created = [];
  state.updated = [];
  state.status = [];
  state.toastError.mockReset();
});

async function choose(trigger: HTMLElement, label: string | RegExp) {
  await userEvent.click(trigger);
  await userEvent.click(await screen.findByRole("option", { name: label }));
}

describe("OrgPage", () => {
  it("creates a relationship from Relationships without adding a team", async () => {
    state.structures = [structure({})];
    renderWithI18n(<OrgPage />);
    await userEvent.click(screen.getByRole("tab", { name: "Relationships" }));
    expect(screen.queryByRole("button", { name: "Add team" })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Create a relationship" }));
    const dialog = screen.getByRole("dialog");
    await choose(within(dialog).getByRole("combobox", { name: "From team" }), "Dev");
    await choose(within(dialog).getByRole("combobox", { name: "Connection type" }), "Escalates to");
    await choose(within(dialog).getByRole("combobox", { name: "Target team" }), "Lead");
    await userEvent.click(within(dialog).getByRole("button", { name: "Add connection" }));
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    const saved = (state.updated[0] as { data: { definition: OrgDefinition } }).data.definition;
    expect(saved.units).toHaveLength(2);
    expect(saved.edges).toContainEqual({ from: "dev", to: "lead", kind: "escalates_to" });
  });

  it("assigns from Directory and returns there after editing an owner's team", async () => {
    state.structures = [structure({})];
    renderWithI18n(<OrgPage />);
    await userEvent.click(screen.getByRole("tab", { name: "Directory" }));
    expect(screen.queryByRole("button", { name: "Add team" })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Owner of Lead" }));
    expect(screen.getByRole("combobox", { name: "Human owner" })).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "Back to Directory" }));
    expect(screen.getByRole("tab", { name: "Directory" })).toHaveAttribute("aria-selected", "true");
    fireEvent.change(screen.getByRole("textbox", { name: "Find a person or agent" }), { target: { value: "Sol" } });
    const card = screen.getByRole("article");
    await userEvent.click(within(card).getByRole("button", { name: "Assign a person" }));
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByRole("combobox", { name: "Person or agent" })).toHaveTextContent("Sol");
    await choose(within(dialog).getByRole("combobox", { name: "Assign to a team" }), "Dev");
    await userEvent.click(within(dialog).getByRole("button", { name: "Add to this team" }));
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    const saved = (state.updated[0] as { data: { definition: OrgDefinition } }).data.definition;
    expect(saved.units).toHaveLength(2);
    expect(saved.units[1]?.members).toContainEqual({ type: "agent", id: "a-3" });
    expect(state.created).toEqual([]);
  });

  it("assigns an existing agent from the directory without creating a team or changing its owner", async () => {
    state.structures = [structure({})];
    renderWithI18n(<OrgPage />);
    const directory = screen.getByRole("complementary", { name: "People to assign" });
    fireEvent.click(within(directory).getByRole("button", { name: "Sol" }));
    await choose(within(directory).getByRole("combobox", { name: "Assign to a team" }), "Dev");
    fireEvent.click(within(directory).getByRole("button", { name: "Add to this team" }));
    expect(screen.getAllByTestId("org-team")).toHaveLength(2);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    const saved = (state.updated[0] as { data: { definition: OrgDefinition } }).data;
    expect(saved.definition.units.find(u => u.id === "dev")?.members).toContainEqual({ id: "a-3", type: "agent" });
    expect(saved.definition.units.find(u => u.id === "dev")?.owner_id).toBeUndefined();
    expect(state.created).toEqual([]);
  });

  it("lists structures, the workspace default first, with model labels and project titles", async () => {
    state.structures = [
      structure({ id: "proj", project_id: "p-1", model: "market", name: "Apollo market", status: "active", paused_units: ["dev"] }),
      structure({ id: "def", name: "Default org" }),
    ];
    renderWithI18n(<OrgPage />);
    expect(screen.getByTestId("org-structure-picker")).toHaveTextContent("Default org");
    expect(screen.getAllByTestId("org-team")).toHaveLength(2);
    await choose(screen.getByTestId("org-structure-picker"), /Apollo market/);
    expect(screen.getByRole("heading", { name: "Apollo market" })).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "All organizations" }));
    const rows = screen.getAllByTestId("org-structure");
    expect(rows[0]?.textContent).toContain("Workspace default");
    expect(rows[1]?.textContent).toContain("Apollo");

  });

  it("opens the need-first creation wizard", async () => {
    renderWithI18n(<OrgPage />);
    fireEvent.click(screen.getAllByRole("button", { name: "New organization" })[0]!);
    expect(await screen.findByLabelText("In one sentence")).toBeVisible();
    expect(state.created).toHaveLength(0);
  });

  it("opens the detail with the chart, blocks save on invalid JSON, and saves the parsed definition", () => {
    state.structures = [structure({ id: "s" })];
    renderWithI18n(<OrgPage />);
    expect(screen.getAllByTestId("org-team")).toHaveLength(2);
    fireEvent.click(screen.getByRole("tab", { name: "Settings" }));
    fireEvent.click(screen.getByText("Advanced: JSON definition"));
    const editor = screen.getByLabelText("Definition (JSON)");
    fireEvent.change(editor, { target: { value: "{ nope" } });
    expect(screen.getByRole("alert").textContent).toMatch(/^Invalid JSON/);
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    fireEvent.change(editor, { target: { value: JSON.stringify({ ...definition, units: [definition.units[0]], edges: [] }) } });
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Renamed" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(state.updated[0]).toMatchObject({ id: "s", data: { name: "Renamed", owner_id: "u-1", dissolve_at: "", end_condition: "", budget_usd_ticks: 0 } });
    expect((state.updated[0] as { data: { definition: OrgDefinition } }).data.definition.units).toHaveLength(1);
  });

  it("keeps a team draft when browsing another organization and returning", async () => {
    state.structures = [structure({ id: "s" })];
    renderWithI18n(<OrgPage />);
    fireEvent.click(within(screen.getAllByTestId("org-team")[0]!).getAllByRole("button")[0]!);
    fireEvent.change(screen.getByLabelText("Team name"), { target: { value: "New lead" } });
    fireEvent.click(screen.getByRole("button", { name: "Back to Teams" }));
    expect(screen.getAllByTestId("org-team")[0]?.textContent).toContain("New lead");
    expect(screen.getByRole("button", { name: "Activate" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "All organizations" }));
    fireEvent.click(screen.getByTestId("org-structure"));
    expect(screen.getAllByTestId("org-team")[0]?.textContent).toContain("New lead");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(state.updated[0]).toMatchObject({ id: "s", data: { expected_revision: 1, definition: { units: [expect.objectContaining({ name: "New lead" }), expect.anything()] } } });
  });

  it("adds an existing agent to the selected team without creating an extra team or manager", async () => {
    state.structures = [structure({ id: "s" })];
    renderWithI18n(<OrgPage />);
    fireEvent.click(within(screen.getAllByTestId("org-team")[0]!).getByRole("button", { name: "Add members" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("checkbox", { name: /Sol/ }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Add selection (1)" }));
    expect(screen.getAllByTestId("org-team")).toHaveLength(2);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(state.updated[0]).toMatchObject({ data: { definition: { edges: definition.edges, units: [expect.objectContaining({ id: "lead", members: [{ type: "member", id: "u-1" }, { type: "agent", id: "a-3" }] }), expect.anything()] } } });
  });

  it("creates an empty team only after an explicit name and preserves its independent parent and owner choices", async () => {
    state.structures = [structure({ id: "s" })];
    renderWithI18n(<OrgPage />);
    fireEvent.click(screen.getAllByRole("button", { name: "Add team" })[0]!);
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("button", { name: "Add team" })).toBeDisabled();
    expect(within(dialog).getByRole("combobox", { name: "Human owner" })).toHaveTextContent("No owner");
    expect(within(dialog).getByRole("combobox", { name: "Parent team" })).toHaveTextContent("No parent team");
    fireEvent.change(within(dialog).getByLabelText("Team name"), { target: { value: "Operations" } });
    await choose(within(dialog).getByRole("combobox", { name: "Parent team" }), "Lead");
    fireEvent.click(within(dialog).getByRole("button", { name: "Add team" }));
    expect(screen.getAllByTestId("org-team")).toHaveLength(3);
    expect(within(screen.getByRole("dialog")).getByText("This team is empty. Add its first members.")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Back to Teams" }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    const change = state.updated[0] as { data: { definition: OrgDefinition } };
    expect(change.data.definition.units[2]).toMatchObject({ name: "Operations", members: [] });
    expect(change.data.definition.units[2]?.owner_id).toBeUndefined();
    expect(change.data.definition.edges.at(-1)?.to).toBe("lead");
  });

  it("restores a changed membership draft after remounting", () => {
    state.structures = [structure({ id: "s" })];
    const mounted = renderWithI18n(<OrgPage />);
    fireEvent.click(within(screen.getAllByTestId("org-team")[0]!).getAllByRole("button")[0]!);
    fireEvent.change(screen.getByLabelText("Team name"), { target: { value: "Recovered name" } });
    mounted.unmount(); renderWithI18n(<OrgPage />);
    expect(screen.getAllByTestId("org-team")[0]?.textContent).toContain("Recovered name");
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
  });

  it("activates with the attestation after showing the preflight numbers", async () => {
    state.structures = [structure({ id: "s" })];
    renderWithI18n(<OrgPage />);
    fireEvent.click(screen.getByRole("button", { name: "Activate" }));
    const dialog = await screen.findByRole("dialog");
    const pre = within(dialog).getByTestId("org-preflight").textContent;
    expect(pre).toContain("manager → workers");
    expect(pre).toContain("$1.50");
    expect(pre).toContain("90 s");
    const submit = within(dialog).getByRole("button", { name: "Activate" });
    expect(submit).toBeDisabled();
    fireEvent.change(within(dialog).getByLabelText("Eval attestation"), { target: { value: "30 cases green on 2026-09-04" } });
    fireEvent.click(submit);
    expect(state.status[0]).toEqual({ id: "s", action: "activate", eval_attestation: "30 cases green on 2026-09-04" });
  });

  it("renders health counters, unit rows and proposals", () => {
    state.structures = [structure({ id: "s", status: "active" })];
    state.health = {
      structure_id: "s", window_days: 7, routed: 12, unrouted: 1, escalations: 2, stacked_escalations: 0, reassigned_outside: 1, market_short: 0, breakers: 0, human_review_items: 3, drift_rate: 0.25,
      units: [{ unit_id: "dev", name: "Dev", routed: 10, escalations: 2, reassigned_outside: 1, vacant_roles: ["reviewer"], saturated_agents: ["a-1"], paused: false, spend_usd_ticks: 2_000_000, budget_usd_ticks: 5_000_000, human_review_items: 1 }],
      proposals: [{ key: "vacant-dev", unit_id: "dev", title: "Fill the reviewer role", body: "Dev has had no reviewer for 7 days.", measure: "vacant_roles = 0" }],
    };
    renderWithI18n(<OrgPage />);
    fireEvent.click(screen.getByRole("tab", { name: "Activity" }));
    const health = screen.getByTestId("org-health").textContent;
    expect(health).toContain("Drift rate25%");
    expect(screen.getByTestId("org-health-unit").textContent).toContain("reviewer");
    expect(screen.getByTestId("org-health-unit").textContent).toContain("$2.00 / $5.00");
    expect(screen.getByTestId("org-proposal").textContent).toContain("Fill the reviewer role");
    expect(screen.getByTestId("org-proposal").textContent).toContain("Measure: vacant_roles = 0");
  });
});
