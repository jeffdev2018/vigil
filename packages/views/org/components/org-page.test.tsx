// @vitest-environment jsdom
import { useState } from "react";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import type { OrgDefinition, OrgHealth, OrgStructure, OrgTemplate } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Mermaid source and model labels: packages/core/org/queries.test.ts.
// ActorAvatar is stubbed below: it calls useWorkspacePaths() unconditionally
// (throws without a WorkspaceSlugProvider) and only decorates a name PersonName
// already renders through useActorName, so nothing under test loses coverage.

const zeroHealth: OrgHealth = {
  structure_id: "s", window_days: 7, routed: 0, unrouted: 0, escalations: 0, stacked_escalations: 0,
  reassigned_outside: 0, market_short: 0, breakers: 0, human_review_items: 0, drift_rate: 0, units: [], proposals: [],
};

const state = vi.hoisted(() => ({
  structures: [] as OrgStructure[],
  templates: [] as OrgTemplate[],
  health: null as OrgHealth | null,
  members: [] as unknown[],
  simulationResult: null as unknown,
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
vi.mock("sonner", () => ({ toast: { error: state.toastError, success: vi.fn() } }));
vi.mock("../../settings/components/export-import-setting", () => ({ ExportImportSetting: () => null }));
vi.mock("./org-team-catalog", () => ({ OrgTeamCatalog: () => null }));
vi.mock("../../modals/issue-picker-modal", () => ({ IssuePickerModal: () => null }));
// Real useWorkspacePaths() throws outside a WorkspaceSlugProvider; the page
// never asserts on the avatar image itself (names come from PersonName /
// useActorName directly), so stubbing it out is the simplest, most faithful
// stand-in rather than adding a routing provider nothing else needs.
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
// Spread the real module: `queryOptions`, `useMutation`, `useQueryClient` etc.
// must stay real for every hook this page tree calls that we do not special-case
// below (e.g. useActorName's plugin lookup) — mocking `useQuery` alone,
// without keeping `queryOptions`, is what made the previous version of this
// file crash before a single test could run.
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: (o: { queryKey?: readonly unknown[] }) => {
    const [key, id] = o.queryKey ?? [];
    if (key === "org-list") return { data: state.structures, isLoading: false, isPending: false };
    if (key === "org-detail") {
      const structure = state.structures.find((s) => s.id === id);
      return { data: structure ? { structure, revisions: [{ id: "r1", revision: 1, model: structure.model, status: "draft", note: "", changed_by: null, created_at: "2026-09-01T10:00:00Z" }] } : null, isPending: false };
    }
    if (key === "org-templates") return { data: state.templates, isPending: false };
    if (key === "org-health") return { data: state.health, isPending: false, isError: false, refetch: vi.fn() };
    if (key === "org-preflight") return { data: { model: "hierarchy", pattern: "manager → workers", coordination_runs_per_issue: 2, coordination_cost_usd_ticks_per_issue: 15_000_000_000, human_review_items_per_issue: 1, human_review_seconds_per_issue: 90, units: 2, units_without_owner: 0, agents: 3, activation_requirements: [] } };
    if (key === "agents") return { data: [{ id: "a-1", name: "Mika", trust_mode: "approval" }, { id: "a-2", name: "Nia", trust_mode: "approval" }, { id: "a-3", name: "Sol", trust_mode: "approval" }] };
    if (key === "members") return { data: state.members, isLoading: false };
    if (key === "projects") return { data: [{ id: "p-1", title: "Apollo" }], isLoading: false };
    if (key === "goals") return { data: [] };
    if (key === "squads") return { data: [] };
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
  // A minimal, reactive stand-in for the real mutation: `mutate` records the
  // request OrgTester sent (so its own "stale" check still lines up) and hands
  // back whatever this test staged as the response.
  useSimulateOrg: () => {
    const [sim, setSim] = useState<{ data: unknown; variables: unknown }>({ data: undefined, variables: undefined });
    return {
      data: sim.data,
      variables: sim.variables,
      isPending: false,
      error: null,
      mutate: (req: unknown) => setSim({ data: state.simulationResult, variables: req }),
    };
  },
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
  state.health = zeroHealth;
  state.members = [{ user_id: "u-1", name: "Ada", role: "owner" }];
  state.simulationResult = null;
  state.created = [];
  state.updated = [];
  state.status = [];
  state.toastError.mockReset();
});

async function choose(trigger: HTMLElement, label: string | RegExp) {
  await userEvent.click(trigger);
  await userEvent.click(await screen.findByRole("option", { name: label }));
}

/** The chart draws one `role="region"` landmark per team, named "Team {name}". */
const teamRegion = (name: string) => screen.getByRole("region", { name: `Team ${name}` });
/** The region's own select-team header is always its first button. */
const selectTeam = (name: string) => fireEvent.click(within(teamRegion(name)).getAllByRole("button")[0]!);
const openStructurePicker = () => fireEvent.click(screen.getByTestId("org-structure-picker"));
const openMoreActions = () => fireEvent.click(screen.getByRole("button", { name: "More actions" }));

describe("OrgPage", () => {
  // OrgPage's first render in a worker carries the module's cold start (lazy
  // imports, i18n resources, JIT): ~1 s here and several under a loaded
  // full-suite run, charged to whichever test happened to run first. Pay it
  // once, outside any test's 5 s budget.
  beforeAll(() => {
    state.structures = [structure({})];
    state.health = zeroHealth;
    state.members = [{ user_id: "u-1", name: "Ada", role: "owner" }];
    renderWithI18n(<OrgPage />);
    cleanup();
  });

  it("creates a relationship from a team's outgoing links without adding a team", async () => {
    state.structures = [structure({})];
    renderWithI18n(<OrgPage />);
    expect(screen.getAllByRole("region")).toHaveLength(2);
    selectTeam("Lead");
    await choose(screen.getByRole("combobox", { name: "Connection type" }), "Escalates to");
    await choose(screen.getByRole("combobox", { name: "Target team" }), "Dev");
    fireEvent.click(screen.getByRole("button", { name: "Add connection" }));
    expect(screen.getAllByRole("region")).toHaveLength(2);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    const saved = (state.updated[0] as { data: { definition: OrgDefinition } }).data.definition;
    expect(saved.units).toHaveLength(2);
    expect(saved.edges).toContainEqual({ from: "lead", to: "dev", kind: "escalates_to" });
  });

  it("assigns from the team directory and returns to the chart", async () => {
    state.structures = [structure({})];
    renderWithI18n(<OrgPage />);
    fireEvent.click(within(teamRegion("Dev")).getByRole("button", { name: "Add" }));
    const sheet = await screen.findByRole("dialog");
    fireEvent.change(within(sheet).getByRole("textbox", { name: "Search a person or an agent" }), { target: { value: "Sol" } });
    fireEvent.click(within(sheet).getByRole("checkbox", { name: /Sol/ }));
    fireEvent.click(within(sheet).getByRole("button", { name: "Add 1 person" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(teamRegion("Dev")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    const saved = (state.updated[0] as { data: { definition: OrgDefinition } }).data.definition;
    expect(saved.units.find((u) => u.id === "dev")?.members).toContainEqual({ type: "agent", id: "a-3" });
  });

  it("assigns an existing agent from the directory without creating a team or changing its owner", async () => {
    state.structures = [structure({})];
    renderWithI18n(<OrgPage />);
    fireEvent.click(within(teamRegion("Lead")).getByRole("button", { name: "Add" }));
    const sheet = await screen.findByRole("dialog");
    fireEvent.click(within(sheet).getByRole("checkbox", { name: /Sol/ }));
    fireEvent.click(within(sheet).getByRole("button", { name: "Add 1 person" }));
    expect(screen.getAllByRole("region")).toHaveLength(2);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    const saved = (state.updated[0] as { data: { definition: OrgDefinition } }).data.definition;
    expect(saved.units.find((u) => u.id === "lead")?.members).toContainEqual({ type: "agent", id: "a-3" });
    expect(saved.units.find((u) => u.id === "lead")?.owner_id).toBe("u-1");
    expect(state.created).toEqual([]);
  });

  it("lists structures, the workspace default first, with model labels and project titles", async () => {
    state.structures = [
      structure({ id: "proj", project_id: "p-1", model: "market", name: "Apollo market", status: "active", paused_units: ["dev"] }),
      structure({ id: "def", name: "Default org" }),
    ];
    renderWithI18n(<OrgPage />);
    expect(screen.getByTestId("org-structure-picker")).toHaveTextContent("Default org");
    expect(screen.getAllByRole("region")).toHaveLength(2);
    openStructurePicker();
    fireEvent.click(await screen.findByRole("menuitem", { name: /Apollo market/ }));
    expect(screen.getByRole("heading", { name: "Apollo market", level: 1 })).toBeVisible();
    openStructurePicker();
    fireEvent.click(await screen.findByRole("menuitem", { name: "All organizations" }));
    const rows = screen.getAllByTestId("org-structure");
    expect(rows[0]?.textContent).toContain("Workspace default");
    expect(rows[1]?.textContent).toContain("Apollo");
  });

  it("opens the need-first creation wizard", async () => {
    state.structures = [structure({})];
    renderWithI18n(<OrgPage />);
    openStructurePicker();
    fireEvent.click(await screen.findByRole("menuitem", { name: "New organization" }));
    expect(await screen.findByLabelText("In one sentence")).toBeVisible();
    expect(state.created).toHaveLength(0);
  });

  it("opens the detail with the chart, blocks save on invalid JSON, and saves the parsed definition", () => {
    state.structures = [structure({ id: "s" })];
    renderWithI18n(<OrgPage />);
    expect(screen.getAllByRole("region")).toHaveLength(2);
    fireEvent.click(screen.getByText("Advanced: JSON definition"));
    const editor = screen.getByLabelText("Definition (JSON)");
    fireEvent.change(editor, { target: { value: "{ nope" } });
    // One alert only: the message beside the editor. The chart's fallback is a
    // lasting state, not an event, so a screen reader does not hear it twice.
    expect(screen.getByRole("alert")).toHaveTextContent(/Invalid JSON/);
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    fireEvent.change(editor, { target: { value: JSON.stringify({ ...definition, units: [definition.units[0]], edges: [] }) } });
    fireEvent.click(screen.getByText("Settings"));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Renamed" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(state.updated[0]).toMatchObject({ id: "s", data: { name: "Renamed", owner_id: "u-1", dissolve_at: "", end_condition: "", budget_usd_ticks: 0 } });
    expect((state.updated[0] as { data: { definition: OrgDefinition } }).data.definition.units).toHaveLength(1);
  });

  it("keeps a team draft when browsing another organization and returning", async () => {
    state.structures = [structure({ id: "s" })];
    renderWithI18n(<OrgPage />);
    selectTeam("Lead");
    fireEvent.change(screen.getByLabelText("Team name"), { target: { value: "New lead" } });
    expect(teamRegion("New lead")).toBeVisible();
    openStructurePicker();
    fireEvent.click(await screen.findByRole("menuitem", { name: "All organizations" }));
    fireEvent.click(screen.getByTestId("org-structure"));
    expect(teamRegion("New lead")).toBeVisible();
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
  });

  it("adds an existing agent to the selected team without creating an extra team or manager", async () => {
    state.structures = [structure({ id: "s" })];
    renderWithI18n(<OrgPage />);
    selectTeam("Dev");
    fireEvent.click(screen.getByRole("button", { name: "Add members" }));
    const sheet = await screen.findByRole("dialog");
    fireEvent.click(within(sheet).getByRole("checkbox", { name: /Sol/ }));
    fireEvent.click(within(sheet).getByRole("button", { name: "Add 1 person" }));
    expect(screen.getAllByRole("region")).toHaveLength(2);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    const saved = (state.updated[0] as { data: { definition: OrgDefinition } }).data.definition;
    expect(saved.units.find((u) => u.id === "dev")?.members).toContainEqual({ type: "agent", id: "a-3" });
    expect(saved.units.find((u) => u.id === "dev")?.owner_id).toBeUndefined();
  });

  it("creates an empty team only after an explicit name and preserves its independent parent and owner choices", async () => {
    state.structures = [structure({ id: "s" })];
    renderWithI18n(<OrgPage />);
    fireEvent.click(screen.getByRole("button", { name: "Add a team" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("button", { name: "Add the team" })).toBeDisabled();
    expect(within(dialog).getByLabelText("Lead")).toHaveTextContent("No lead yet");
    fireEvent.change(within(dialog).getByLabelText("Name"), { target: { value: "Operations" } });
    // Name alone is not enough: this organization is a hierarchy with existing
    // teams, so a parent is required before the team can be created.
    expect(within(dialog).getByRole("button", { name: "Add the team" })).toBeDisabled();
    await choose(within(dialog).getByLabelText("Reports to (required in a hierarchy)"), "Lead");
    expect(within(dialog).getByRole("button", { name: "Add the team" })).toBeEnabled();
    fireEvent.click(within(dialog).getByRole("button", { name: "Add the team" }));
    expect(screen.getAllByRole("region")).toHaveLength(3);
    expect(teamRegion("Operations")).toBeVisible();
    // Choosing a parent never implied an owner: the new team's own panel opens
    // selected, still unowned.
    expect(screen.getByLabelText("Human owner")).toHaveTextContent("No owner");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    const change = state.updated[0] as { data: { definition: OrgDefinition } };
    const created = change.data.definition.units[2];
    expect(created).toMatchObject({ name: "Operations", members: [] });
    expect(created?.owner_id).toBeUndefined();
    expect(change.data.definition.edges.at(-1)).toEqual({ from: created?.id, to: "lead", kind: "reports_to" });
  });

  it("restores a changed membership draft after remounting", () => {
    state.structures = [structure({ id: "s" })];
    const mounted = renderWithI18n(<OrgPage />);
    selectTeam("Dev");
    expect(within(teamRegion("Dev")).getByText("Nia")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Remove Nia from this team" }));
    expect(within(teamRegion("Dev")).queryByText("Nia")).not.toBeInTheDocument();
    mounted.unmount();
    renderWithI18n(<OrgPage />);
    expect(within(teamRegion("Dev")).queryByText("Nia")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
  });

  it("activates with the attestation after showing the preflight numbers", async () => {
    state.structures = [structure({ id: "s" })];
    renderWithI18n(<OrgPage />);
    openMoreActions();
    fireEvent.click(await screen.findByRole("menuitem", { name: "Activate" }));
    const dialog = await screen.findByRole("dialog");
    const pre = within(dialog).getByTestId("org-preflight").textContent;
    // orgModelDescription wins over the server's raw `pattern` string whenever
    // the model has a known sentence — see labels.ts `orgModelDescription`.
    expect(pre).toContain("A clear chain: every team knows who to escalate to.");
    expect(pre).toContain("$1.50");
    expect(pre).toContain("90 s");
    const submit = within(dialog).getByRole("button", { name: "Activate" });
    expect(submit).toBeDisabled();
    fireEvent.change(within(dialog).getByLabelText("Eval attestation"), { target: { value: "30 cases green on 2026-09-04" } });
    fireEvent.click(submit);
    expect(state.status[0]).toEqual({ id: "s", action: "activate", eval_attestation: "30 cases green on 2026-09-04" });
  });

  it("shows non-zero health counters and a proposal, filters out the vacant-roles proposal, and shows a sentence when nothing happened", () => {
    state.structures = [structure({ id: "s", status: "active" })];
    state.health = {
      structure_id: "s", window_days: 7, routed: 12, unrouted: 1, escalations: 2, stacked_escalations: 0, reassigned_outside: 1, market_short: 0, breakers: 0, human_review_items: 3, drift_rate: 0.25,
      units: [{ unit_id: "dev", name: "Dev", routed: 10, escalations: 2, reassigned_outside: 1, vacant_roles: [], saturated_agents: [], paused: false, spend_usd_ticks: 0, budget_usd_ticks: 0, human_review_items: 1 }],
      proposals: [
        { key: "vacant-dev", unit_id: "dev", title: "Roles vacant in Dev", body: "Fill the roles or remove them: reviewer", measure: "1 of 2 roles vacant", code: "vacant_roles" },
        { key: "esc-dev", unit_id: "dev", title: "Unit Dev escalates too much", body: "Add a backup member, split the unit, or widen its allow list so fewer issues go up.", measure: "2 escalations in 7 days (quota 1/day)", code: "escalations" },
      ],
    };
    const mounted = renderWithI18n(<OrgPage />);
    const health = screen.getByTestId("org-health").textContent;
    expect(health).toContain("Escalations");
    expect(health).toContain("2");
    const proposals = screen.getAllByTestId("org-proposal");
    expect(proposals).toHaveLength(1);
    expect(proposals[0]?.textContent).toContain("escalates too much");
    expect(screen.queryByTestId("org-health-empty")).not.toBeInTheDocument();
    mounted.unmount();

    state.structures = [structure({ id: "s2", status: "active" })];
    state.health = zeroHealth;
    renderWithI18n(<OrgPage />);
    expect(screen.getByTestId("org-health-empty")).toHaveTextContent(
      "No request routed in the last 7 days. Test one to see the path it would take.",
    );
    expect(screen.queryByTestId("org-health")).not.toBeInTheDocument();
  });

  it("hides every edit control from a member who is neither owner nor admin, and explains why", () => {
    state.structures = [structure({ id: "s" })];
    state.members = [{ user_id: "u-1", name: "Ada", role: "member" }];
    renderWithI18n(<OrgPage />);
    expect(screen.queryByRole("button", { name: "Add a team" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Save" })).not.toBeInTheDocument();
    openMoreActions();
    expect(screen.queryByRole("menuitem", { name: "Activate" })).not.toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Import or export" })).not.toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "History" })).toBeInTheDocument();
    expect(screen.getByText("Only a workspace owner or admin can change this organization.")).toBeVisible();
  });

  it("does not show Publish until there is something to publish", () => {
    state.structures = [structure({ id: "s", status: "active" })];
    renderWithI18n(<OrgPage />);
    expect(screen.queryByRole("button", { name: "Publish" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Save" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByText("Settings"));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Renamed org" } });
    expect(screen.getByRole("button", { name: "Publish" })).toBeEnabled();
  });

  it("lights the tested request's path on the chart: the receiving team carries badge 1, the rest dim", async () => {
    const definition3: OrgDefinition = {
      ...definition,
      units: [...definition.units, { id: "ops", name: "Ops", excludes: ["external_effects"], autonomy: "draft", allow: [], deny: [], escalation_quota_per_day: 3, members: [], roles: [] }],
      edges: [...definition.edges, { from: "ops", to: "lead", kind: "reports_to" }],
    };
    state.structures = [structure({ id: "s", definition: definition3 })];
    state.simulationResult = {
      basis: "draft", structure_id: "s", revision: 1, unit: { id: "dev", name: "Dev", model: "hierarchy", autonomy: "draft" },
      receives: { unit_id: "dev", unit_name: "Dev" }, prepares: { kind: "agent", id: "a-1", name: "Mika" }, decides: { kind: "member", id: "u-1", name: "Ada" },
      escalation_path: [{ unit_id: "lead", unit_name: "Lead" }], blocking_denies: [], cost_estimate_usd_ticks: 0, notes: [], note_codes: [],
    };
    renderWithI18n(<OrgPage />);
    fireEvent.click(screen.getByRole("button", { name: "Test a request" }));
    fireEvent.change(screen.getByLabelText("The request"), { target: { value: "Card declined at checkout" } });
    fireEvent.click(screen.getByRole("button", { name: "Test" }));

    await waitFor(() => expect(within(teamRegion("Dev")).getByText("1")).toBeInTheDocument());
    expect(teamRegion("Ops").className).toContain("opacity-40");
    expect(teamRegion("Lead").className).not.toContain("opacity-40");
    expect(teamRegion("Dev").className).not.toContain("opacity-40");
  });
});
