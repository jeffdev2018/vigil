// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { OrgDefinition, OrgStructure, OrgTemplate } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// The decision table, the template choice and the definition it builds are
// covered once in packages/core/org/templates.test.ts. This file keeps the
// walkthrough, the wiring to POST /api/org and the disabled states.

const state = vi.hoisted(() => ({
  structures: [] as OrgStructure[],
  templates: [] as OrgTemplate[],
  created: [] as Record<string, unknown>[],
  updated: [] as Record<string, unknown>[],
  closed: 0,
  createdId: [] as string[],
  toastError: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/workspace/queries", () => ({ memberListOptions: () => ({ queryKey: ["members"] }), agentListOptions: () => ({ queryKey: ["agents"] }) }));
vi.mock("@multica/core/projects/queries", () => ({ projectListOptions: () => ({ queryKey: ["projects"] }) }));
vi.mock("sonner", () => ({ toast: { error: state.toastError, success: vi.fn() } }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: (o: { queryKey?: readonly unknown[] }) => {
    const [key] = o.queryKey ?? [];
    if (key === "org-list") return { data: state.structures, isPending: false };
    if (key === "org-templates") return { data: state.templates, isPending: false };
    if (key === "projects") return { data: [{ id: "p-1", title: "Apollo" }], isPending: false };
    if (key === "members") return { data: [{ user_id: "u-1", name: "Ada", role: "owner", avatar_url: null }], isPending: false };
    if (key === "agents") return { data: [{ id: "a-1", name: "Mika", avatar_url: null }, { id: "a-2", name: "Nia", avatar_url: null }], isPending: false };
    return { data: undefined, isPending: true };
  },
}));
vi.mock("@multica/core/org", () => ({
  orgListOptions: () => ({ queryKey: ["org-list"] }),
  orgTemplatesOptions: () => ({ queryKey: ["org-templates"] }),
  useCreateOrgStructure: () => ({
    isPending: false,
    mutate: (data: Record<string, unknown>, o: { onSuccess: (s: unknown) => void }) => { state.created.push(data); o.onSuccess({ id: "s-new" }); },
  }),
  useUpdateOrgStructure: () => ({
    isPending: false,
    mutate: (v: Record<string, unknown>, o: { onSuccess: () => void }) => { state.updated.push(v); o.onSuccess(); },
  }),
}));

import { useOrgWizardDraftStore } from "@multica/core/org/draft-store";
import { OrgWizard } from "./org-wizard";

const definition: OrgDefinition = { units: [], edges: [], rules: [], committees: [], market: { price_cap_usd_ticks: 0, offers_per_agent_per_day: 0, min_offers: 0 } };

const template = (over: Partial<OrgTemplate>): OrgTemplate => ({
  model: "circles", name: "Circles", pattern: "handoff by role", description: "Roles, not titles.", coordination_runs_per_issue: 1, definition, ...over,
});

const structure = (over: Partial<OrgStructure>): OrgStructure => ({
  id: "s-default", workspace_id: "ws-1", project_id: null, model: "owner_network", name: "Owner network", status: "active", revision: 1, revision_id: "r1",
  definition, owner_id: "u-1", dissolve_at: null, end_condition: "", budget_usd_ticks: 0, eval_attestation: "", paused_reason: "", dissolved_at: null,
  paused_units: [], created_by: null, created_at: "2026-09-01T10:00:00Z", updated_at: "2026-09-01T10:00:00Z", ...over,
});

const render = () => renderWithI18n(<OrgWizard onClose={() => { state.closed += 1; }} onCreated={(id) => state.createdId.push(id)} />);

const next = (): HTMLButtonElement => screen.getByRole("button", { name: "Next" }) as HTMLButtonElement;

/** The decision table, answered the way the wizard expects it. */
async function answerFlow(user: ReturnType<typeof userEvent.setup>, over: { decider?: string; end?: string; compete?: string } = {}) {
  await user.click(screen.getByRole("radio", { name: over.decider ?? "One person, always the same" }));
  const groups = screen.getAllByRole("group");
  const endGroup = groups.find((g) => g.textContent?.startsWith("A mission with an end?")) as HTMLElement;
  const competeGroup = groups.find((g) => g.textContent?.startsWith("Put the agents in competition?")) as HTMLElement;
  await user.click(within(endGroup).getByRole("radio", { name: over.end ?? "No" }));
  await user.click(within(competeGroup).getByRole("radio", { name: over.compete ?? "No" }));
}

beforeEach(() => {
  useOrgWizardDraftStore.getState().clearDraft();
  state.structures = [];
  state.templates = [];
  state.created = [];
  state.updated = [];
  state.closed = 0;
  state.createdId = [];
  state.toastError.mockReset();
});

describe("OrgWizard", () => {
  it("walks the four steps from the keyboard and posts a draft routed on the sentence's words", async () => {
    const user = userEvent.setup();
    render();

    // 1 — the sentence. Next stays disabled until there is one.
    expect(next().disabled).toBe(true);
    await user.click(screen.getByLabelText("In one sentence"));
    await user.keyboard("Répondre aux tickets de nos clients");
    expect(next().disabled).toBe(false);
    await user.keyboard("{Tab}");
    await user.click(next());

    // 2 — the decision table, one answer at a time.
    expect(next().disabled).toBe(true);
    await answerFlow(user);
    expect(screen.getByTestId("org-wizard-model").textContent).toContain("Hierarchy");
    expect(screen.getByTestId("org-wizard-model").textContent).toContain("A clear chain");
    await user.click(next());

    // 3 — the real members of the workspace, on the units of the support template.
    expect(screen.getAllByTestId("org-wizard-actor")).toHaveLength(3);
    expect((screen.getByLabelText("Unit of Ada") as HTMLSelectElement).value).toBe("support-lead");
    expect((screen.getByLabelText("Unit of Mika") as HTMLSelectElement).value).toBe("front-line");
    await user.selectOptions(screen.getByLabelText("Unit of Nia"), "front-line");
    await user.click(next());

    // 4 — the preview, then the draft.
    const units = screen.getAllByTestId("org-wizard-unit").map((u) => u.textContent ?? "");
    expect(units[0]).toContain("Support coordination");
    expect(units[0]).toContain("1 member");
    expect(units[1]).toContain("2 members");
    await user.click(screen.getByRole("button", { name: "Open my draft" }));

    expect(state.created).toHaveLength(1);
    const body = state.created[0] as { model: string; name: string; project_id: null; definition: OrgDefinition };
    expect(body.model).toBe("hierarchy");
    expect(body.project_id).toBe(null);
    expect(body.name).toBe("Répondre Tickets Clients");
    // The words of step 1 land where orgMatchUnit reads them: the unit's rule.
    expect(body.definition.rules.find((r) => r.target_unit === "front-line")?.keywords).toEqual(
      expect.arrayContaining(["support", "répondre", "tickets", "clients"]),
    );
    expect(body.definition.units.find((u) => u.id === "front-line")?.members).toEqual([
      { type: "agent", id: "a-1" },
      { type: "agent", id: "a-2" },
    ]);
    expect(state.createdId).toEqual(["s-new"]);
    expect(state.closed).toBe(1);
  });

  it("lets “choose otherwise” pick a model from the catalogue of the seven structures", async () => {
    const user = userEvent.setup();
    state.templates = [template({ model: "market", name: "Internal market" }), template({}), template({ model: "hierarchy", name: "Hierarchy", composite: true })];
    render();
    fireEvent.change(screen.getByLabelText("In one sentence"), { target: { value: "Analyser les données de l'étude" } });
    await user.click(next());
    await answerFlow(user);
    expect(screen.getByTestId("org-wizard-model").textContent).toContain("Hierarchy");

    await user.click(screen.getByRole("button", { name: "Choose otherwise" }));
    // The composed hierarchy is not one of the seven models the table names.
    expect(screen.getAllByTestId("org-template")).toHaveLength(2);
    await user.click(screen.getAllByTestId("org-template")[0]!);
    expect(screen.getByTestId("org-wizard-model").textContent).toContain("Internal market");

    await user.click(next());
    await user.click(next());
    await user.click(screen.getByRole("button", { name: "Open my draft" }));
    expect((state.created[0] as { model: string }).model).toBe("market");
    // The business template still comes from the sentence, not from the model.
    expect((state.created[0] as { definition: OrgDefinition }).definition.units.map((u) => u.id)).toEqual([
      "research-lead", "research", "peer-review",
    ]);
  });

  it("never replaces the workspace default and creates only in an available scope", async () => {
    const user = userEvent.setup();
    state.structures = [structure({})];
    render();
    fireEvent.change(screen.getByLabelText("In one sentence"), { target: { value: "Suivre les factures" } });
    expect(next()).toBeDisabled();
    expect(screen.getByRole("alert").textContent).toContain("already has an organization");
    await user.selectOptions(screen.getByLabelText("Applies to"), "p-1");
    await user.click(next());
    await answerFlow(user, { decider: "The owner of each topic" });
    await user.click(next());
    await user.selectOptions(screen.getByLabelText("Unit of Mika"), "");
    await user.click(next());
    await user.click(screen.getByRole("button", { name: "Open my draft" }));
    expect(state.updated).toHaveLength(0);
    expect(state.created).toHaveLength(1);
    expect(state.created[0]).toMatchObject({ project_id: "p-1" });
    const def = state.created[0]?.definition as OrgDefinition;
    expect(def.units.flatMap(u => u.members).some(m => m.id === "a-1")).toBe(false);
  });
});
