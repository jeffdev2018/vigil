// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
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
vi.mock("@multica/core/workspace/queries", () => ({ memberListOptions: () => ({ queryKey: ["members"] }) }));
vi.mock("@multica/core/projects/queries", () => ({ projectListOptions: () => ({ queryKey: ["projects"] }) }));
vi.mock("sonner", () => ({ toast: { error: state.toastError, success: vi.fn() } }));
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

import { OrgPage } from "./org-page";

const definition: OrgDefinition = {
  units: [
    { id: "lead", name: "Lead", owner_id: "u-1", excludes: [], autonomy: "approve_payload", allow: [], deny: [], escalation_quota_per_day: 3, members: [{ type: "member", id: "u-1" }], roles: [] },
    { id: "dev", name: "Dev", excludes: [], autonomy: "draft", allow: [], deny: [], escalation_quota_per_day: 3, members: [{ type: "agent", id: "a-1" }, { type: "agent", id: "a-2" }], roles: [] },
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

const template = (over: Partial<OrgTemplate>): OrgTemplate => ({
  model: "squads", name: "Squads", pattern: "peer squads", description: "Small autonomous squads.", coordination_runs_per_issue: 1, definition, ...over,
});

beforeEach(() => {
  state.structures = [];
  state.templates = [];
  state.health = null;
  state.created = [];
  state.updated = [];
  state.status = [];
  state.toastError.mockReset();
});

describe("OrgPage", () => {
  it("lists structures, the workspace default first, with model labels and project titles", () => {
    state.structures = [
      structure({ id: "proj", project_id: "p-1", model: "market", name: "Apollo market", status: "active", paused_units: ["dev"] }),
      structure({ id: "def", name: "Default org" }),
    ];
    renderWithI18n(<OrgPage />);
    const cards = screen.getAllByTestId("org-structure");
    expect(cards[0]?.textContent).toContain("Workspace default");
    expect(cards[0]?.textContent).toContain("Hierarchy");
    expect(cards[0]?.textContent).toContain("Ada");
    expect(cards[1]?.textContent).toContain("Apollo");
    expect(cards[1]?.textContent).toContain("Internal market");
    expect(cards[1]?.textContent).toContain("1 paused unit");
  });

  it("creates a structure from the picked template, with its definition", async () => {
    state.templates = [template({}), template({ model: "circles", name: "Circles", pattern: "circles", description: "Roles, not titles." })];
    renderWithI18n(<OrgPage />);
    fireEvent.click(screen.getAllByRole("button", { name: "New structure" })[0]!);
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Applies to"), { target: { value: "p-1" } });
    const cards = within(dialog).getAllByTestId("org-template");
    expect(cards).toHaveLength(2);
    expect(cards[1]?.textContent).toContain("Circles and roles");
    expect(cards[1]?.textContent).toContain("1 coordination run per issue");
    fireEvent.click(cards[1]!);
    expect(state.created[0]).toEqual({ project_id: "p-1", model: "circles", name: "Circles", definition });
  });

  it("opens the detail with the chart, blocks save on invalid JSON, and saves the parsed definition", () => {
    state.structures = [structure({ id: "s" })];
    renderWithI18n(<OrgPage />);
    fireEvent.click(screen.getByTestId("org-structure"));
    expect(screen.getByTestId("mermaid").textContent).toContain('u_dev -->|reports to| u_lead');
    expect(screen.getAllByTestId("org-unit").map((u) => u.textContent)).toEqual(["LeadAda · Approve payload · 1 member", "DevNo owner · Draft · 2 members"]);
    const editor = screen.getByLabelText("Definition (JSON)");
    fireEvent.change(editor, { target: { value: "{ nope" } });
    expect(screen.getByRole("alert").textContent).toMatch(/^Invalid JSON/);
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    fireEvent.change(editor, { target: { value: JSON.stringify({ ...definition, units: [definition.units[0]] }) } });
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Renamed" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(state.updated[0]).toMatchObject({ id: "s", data: { name: "Renamed", owner_id: "u-1", dissolve_at: null, end_condition: "", budget_usd_ticks: 0 } });
    expect((state.updated[0] as { data: { definition: OrgDefinition } }).data.definition.units).toHaveLength(1);
  });

  it("activates with the attestation after showing the preflight numbers", async () => {
    state.structures = [structure({ id: "s" })];
    renderWithI18n(<OrgPage />);
    fireEvent.click(screen.getByTestId("org-structure"));
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
    fireEvent.click(screen.getByTestId("org-structure"));
    const health = screen.getByTestId("org-health").textContent;
    expect(health).toContain("Drift rate25%");
    expect(screen.getByTestId("org-health-unit").textContent).toContain("reviewer");
    expect(screen.getByTestId("org-health-unit").textContent).toContain("$2.00 / $5.00");
    expect(screen.getByTestId("org-proposal").textContent).toContain("Fill the reviewer role");
    expect(screen.getByTestId("org-proposal").textContent).toContain("Measure: vacant_roles = 0");
  });
});
