// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, within } from "@testing-library/react";
import type { OrgDefinition, OrgHealth, OrgStructure } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Mermaid source and model labels: packages/core/org/queries.test.ts.

const state = vi.hoisted(() => ({
  structures: [] as OrgStructure[],
  health: null as OrgHealth | null,
  created: [] as unknown[],
  updated: [] as unknown[],
  status: [] as unknown[],
  toastError: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/auth", () => ({ useAuthStore: (sel: (s: unknown) => unknown) => sel({ user: { id: "u-1" } }) }));
vi.mock("@multica/core/workspace/queries", () => ({ memberListOptions: () => ({ queryKey: ["members"] }), agentListOptions: () => ({ queryKey: ["agents"] }) }));
vi.mock("@multica/core/goals", () => ({ goalListOptions: () => ({ queryKey: ["goals"] }) }));
vi.mock("@multica/core/runtimes/queries", () => ({ runtimeListOptions: () => ({ queryKey: ["runtimes"] }) }));
// The people chart's presence dots have their own suite (org-people-chart.test.tsx).
vi.mock("@multica/core/agents", () => ({ useWorkspacePresenceMap: () => ({ byAgent: new Map(), loading: false }) }));
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
    if (key === "org-health") return { data: state.health, isPending: false };
    if (key === "org-preflight") return { data: { model: "hierarchy", pattern: "manager → workers", coordination_runs_per_issue: 2, coordination_cost_usd_ticks_per_issue: 1_500_000, human_review_items_per_issue: 1, human_review_seconds_per_issue: 90, units: 2, units_without_owner: 0, agents: 3, activation_requirements: [] } };
    if (key === "members") return { data: [{ user_id: "u-1", name: "Ada", role: "owner", avatar_url: null }], isLoading: false };
    if (key === "agents") return { data: [{ id: "a-1", name: "Mika", avatar_url: null, trust_mode: "autonomous" }, { id: "a-2", name: "Nia", avatar_url: null, trust_mode: "autonomous" }], isLoading: false };
    if (key === "goals") return { data: [], isLoading: false };
    if (key === "runtimes") return { data: [], isLoading: false };
    if (key === "projects") return { data: [{ id: "p-1", title: "Apollo" }], isLoading: false };
    return { data: undefined, isLoading: false, isPending: true };
  },
}));
vi.mock("@multica/core/org", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/org")>()),
  orgListOptions: () => ({ queryKey: ["org-list"] }),
  orgDetailOptions: (_ws: string, id: string) => ({ queryKey: ["org-detail", id] }),
  orgHealthOptions: (_ws: string, id: string) => ({ queryKey: ["org-health", id] }),
  orgPreflightOptions: (_ws: string, id: string) => ({ queryKey: ["org-preflight", id] }),
  useCreateOrgStructure: () => ({ isPending: false, mutate: (data: unknown, o: { onSuccess: (s: unknown) => void }) => { state.created.push(data); o.onSuccess(null); } }),
  useUpdateOrgStructure: () => ({ isPending: false, mutate: (v: unknown, o: { onSuccess: () => void }) => { state.updated.push(v); o.onSuccess(); } }),
  useSetOrgStructureStatus: () => ({ isPending: false, mutate: (v: unknown, o: { onSuccess: () => void }) => { state.status.push(v); o.onSuccess(); } }),
  useDeleteOrgStructure: () => ({ isPending: false, mutate: vi.fn() }),
  // The tester panel has its own suite; here it only has to mount.
  useSimulateOrg: () => ({ isPending: false, mutate: vi.fn(), data: undefined, error: null }),
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

beforeEach(() => {
  state.structures = [];
  state.health = null;
  state.created = [];
  state.updated = [];
  state.status = [];
  state.toastError.mockReset();
});

describe("OrgPage", () => {
  it("opens the live structure's people chart directly, with the others one pick away", () => {
    state.structures = [
      structure({ id: "proj", project_id: "p-1", model: "market", name: "Apollo market", status: "active", paused_units: ["dev"] }),
      structure({ id: "def", name: "Default org" }),
    ];
    renderWithI18n(<OrgPage />);
    const picker = screen.getByTestId("org-structure-picker") as HTMLSelectElement;
    expect(picker.value).toBe("proj");
    expect([...picker.options].map((o) => o.textContent)).toEqual(["Default org · Workspace default · Draft", "Apollo market · Apollo · Active"]);
    // People first: Ada leads, the two agents report to her.
    expect(screen.getAllByTestId("org-person").map((c) => c.getAttribute("data-person-key"))).toEqual(["lead/member:u-1", "dev/agent:a-1", "dev/agent:a-2"]);
    expect(screen.getAllByTestId("org-person-edge")).toHaveLength(2);
    fireEvent.change(picker, { target: { value: "def" } });
    expect((screen.getByTestId("org-structure-picker") as HTMLSelectElement).value).toBe("def");
    expect(screen.getByRole("button", { name: "Activate" })).toBeTruthy();
  });

  // The create flow is the four-step wizard: org-wizard.test.tsx.

  it("opens the detail with the chart, blocks save on invalid JSON, and saves the parsed definition", () => {
    state.structures = [structure({ id: "s" })];
    renderWithI18n(<OrgPage />);
    fireEvent.click(screen.getByRole("tab", { name: "Teams" }));
    expect(screen.getAllByTestId("org-unit-card").map((c) => c.getAttribute("data-unit-id"))).toEqual(["lead", "dev"]);
    expect(screen.getAllByTestId("org-unit").map((u) => u.textContent)).toEqual(["LeadAda · Approve payload · 1 member", "DevNo owner · Draft · 2 members"]);
    fireEvent.click(screen.getByText("Advanced (JSON)"));
    const editor = screen.getByLabelText("Definition (JSON)");
    fireEvent.change(editor, { target: { value: "{ nope" } });
    expect(screen.getByText(/^Invalid JSON/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    fireEvent.change(editor, { target: { value: JSON.stringify({ ...definition, units: [definition.units[0]] }) } });
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Renamed" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(state.updated[0]).toMatchObject({ id: "s", data: { name: "Renamed", owner_id: "u-1", dissolve_at: null, end_condition: "", budget_usd_ticks: 0 } });
    expect((state.updated[0] as { data: { definition: OrgDefinition } }).data.definition.units).toHaveLength(1);
  });

  it("keeps the canvas and the advanced JSON on the same definition, and undoes canvas edits", () => {
    state.structures = [structure({ id: "s" })];
    renderWithI18n(<OrgPage />);
    fireEvent.click(screen.getByRole("tab", { name: "Teams" }));
    fireEvent.click(screen.getByText("Advanced (JSON)"));

    // Canvas -> JSON: renaming from the unit record rewrites the textarea.
    fireEvent.click(screen.getAllByTestId("org-unit-card")[0]!.querySelector("[data-org-card]")!);
    fireEvent.change(within(screen.getByTestId("org-unit-sheet")).getByLabelText("Name"), { target: { value: "Captain" } });
    const editor = screen.getByLabelText("Definition (JSON)") as HTMLTextAreaElement;
    expect(JSON.parse(editor.value).units[0].name).toBe("Captain");

    // Undo pops the canvas edit back off; typing in the JSON is not a step.
    fireEvent.click(screen.getByRole("button", { name: "Undo (1)" }));
    expect(JSON.parse((screen.getByLabelText("Definition (JSON)") as HTMLTextAreaElement).value).units[0].name).toBe("Lead");

    // JSON -> canvas: the textarea is still the same object the cards draw.
    fireEvent.change(editor, {
      target: { value: JSON.stringify({ ...definition, units: [{ ...definition.units[0], name: "Skipper" }] }) },
    });
    expect(screen.getAllByTestId("org-unit-card")).toHaveLength(1);
    expect(screen.getByTestId("org-unit-card").textContent).toContain("Skipper");
  });

  it("autosaves a draft two seconds after the last canvas edit, and never an active structure", () => {
    vi.useFakeTimers();
    try {
      state.structures = [structure({ id: "s", status: "active" })];
      const active = renderWithI18n(<OrgPage />);
      fireEvent.click(screen.getByRole("tab", { name: "Teams" }));
      fireEvent.click(screen.getAllByTestId("org-unit-card")[0]!.querySelector("[data-org-card]")!);
      fireEvent.change(within(screen.getByTestId("org-unit-sheet")).getByLabelText("Name"), { target: { value: "Captain" } });
      vi.advanceTimersByTime(5000);
      expect(state.updated).toHaveLength(0);
      active.unmount();

      state.structures = [structure({ id: "s" })];
      renderWithI18n(<OrgPage />);
      fireEvent.click(screen.getByRole("tab", { name: "Teams" }));
      fireEvent.click(screen.getAllByTestId("org-unit-card")[0]!.querySelector("[data-org-card]")!);
      fireEvent.change(within(screen.getByTestId("org-unit-sheet")).getByLabelText("Name"), { target: { value: "Captain" } });
      expect(state.updated).toHaveLength(0);
      vi.advanceTimersByTime(2000);
      expect((state.updated[0] as { data: { definition: OrgDefinition } }).data.definition.units[0]?.name).toBe("Captain");
    } finally {
      vi.useRealTimers();
    }
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
    const health = screen.getByTestId("org-health").textContent;
    expect(health).toContain("Drift rate25%");
    expect(screen.getByTestId("org-health-unit").textContent).toContain("reviewer");
    expect(screen.getByTestId("org-health-unit").textContent).toContain("$2.00 / $5.00");
    expect(screen.getByTestId("org-proposal").textContent).toContain("Fill the reviewer role");
    expect(screen.getByTestId("org-proposal").textContent).toContain("Measure: vacant_roles = 0");
  });
});
