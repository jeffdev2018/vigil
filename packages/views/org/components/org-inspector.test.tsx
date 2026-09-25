// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Agent, MemberWithUser, OrgDefinition, OrgHealth, OrgStructure } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import type { OrgSettingsForm } from "./org-form";

const NAMES: Record<string, string> = { "member:m1": "Ada", "agent:a1": "Nova" };

const state = vi.hoisted(() => ({
  health: null as unknown,
  members: [] as unknown[],
  agents: [] as unknown[],
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/goals", () => ({ goalListOptions: () => ({ queryKey: ["goals"] }) }));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"] }),
  memberListOptions: () => ({ queryKey: ["members"] }),
  squadListOptions: () => ({ queryKey: ["squads"] }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (type: string, id: string) => NAMES[`${type}:${id}`] ?? id }),
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: (o: { queryKey?: readonly unknown[] }) => {
    const key = o.queryKey?.[0];
    if (key === "org-health") return { data: state.health, isPending: false, isError: false, refetch: vi.fn() };
    if (key === "agents") return { data: state.agents, isPending: false, isError: false, refetch: vi.fn() };
    if (key === "members") return { data: state.members, isPending: false, isError: false, refetch: vi.fn() };
    return { data: [], isPending: false, isError: false, refetch: vi.fn() };
  },
}));
vi.mock("@multica/core/org", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/org")>()),
  orgHealthOptions: () => ({ queryKey: ["org-health"] }),
}));

import { OrgInspector } from "./org-inspector";

const zeroHealth: OrgHealth = {
  structure_id: "s1", window_days: 7, routed: 0, unrouted: 0, escalations: 0, stacked_escalations: 0,
  reassigned_outside: 0, market_short: 0, breakers: 0, human_review_items: 0, drift_rate: 0, units: [], proposals: [],
};

const definition: OrgDefinition = {
  units: [
    {
      id: "u1", name: "Front line", kind: "unit", owner_id: "m1", mission: "Answer tickets",
      excludes: ["external_effects"], autonomy: "draft", allow: ["read"], deny: ["refund"],
      escalation_quota_per_day: 5,
      members: [{ type: "member", id: "m1" }, { type: "agent", id: "a1" }],
      roles: [{ id: "r1", name: "Triage" }],
    },
    {
      id: "u2", name: "Escalation", kind: "unit", owner_id: "m1",
      excludes: ["external_effects"], autonomy: "auto", allow: [], deny: [],
      escalation_quota_per_day: 5, members: [], roles: [],
    },
  ],
  edges: [
    { from: "u1", to: "u2", kind: "reports_to" },
    { from: "u1", to: "u2", kind: "consults" },
  ],
  rules: [{ id: "rule1", target_unit: "u1", keywords: ["ticket", "support"], priority: 1 }],
  committees: [],
  market: { price_cap_usd_ticks: 5_000_000, offers_per_agent_per_day: 3, min_offers: 1 },
};

const structure: OrgStructure = {
  id: "s1", workspace_id: "ws-1", project_id: null, model: "hierarchy", name: "One", status: "active", revision: 3, revision_id: "r1",
  definition, owner_id: "m1", dissolve_at: null, end_condition: "", budget_usd_ticks: 5_000_000, eval_attestation: "", paused_reason: "",
  dissolved_at: null, paused_units: [], created_by: null, created_at: "2026-09-01T10:00:00Z", updated_at: "2026-09-01T10:00:00Z",
};

const form: OrgSettingsForm = { name: "One", owner_id: "m1", dissolve_at: "", end_condition: "", budget: "5000000", model: "hierarchy" };

const members: MemberWithUser[] = [{ id: "mem1", workspace_id: "ws-1", user_id: "m1", role: "owner", created_at: "", name: "Ada", email: "ada@x.com", avatar_url: null }];
const agents: Agent[] = [{ id: "a1", workspace_id: "ws-1", runtime_id: "rt1", name: "Nova", description: "", instructions: "", trust_mode: "propose" } as Agent];

function setup(over: Partial<React.ComponentProps<typeof OrgInspector>> = {}) {
  const onChange = vi.fn();
  const onFormChange = vi.fn();
  const onJsonChange = vi.fn();
  const onSelect = vi.fn();
  const onAddMembers = vi.fn();
  const props = {
    structure, definition, onChange, form, onFormChange,
    jsonText: JSON.stringify(definition, null, 2), onJsonChange, jsonError: null,
    selection: { kind: "org" as const }, onSelect, problems: [], readOnly: false, onAddMembers,
    ...over,
  };
  const result = renderWithI18n(<OrgInspector {...props} />);
  return { ...result, onChange, onFormChange, onJsonChange, onSelect, onAddMembers };
}

beforeEach(() => {
  state.health = zeroHealth;
  state.members = members;
  state.agents = agents;
});

describe("OrgInspector", () => {
  it("renders the organization overview when nothing is selected", () => {
    setup();
    expect(screen.getByText("Organization")).toBeInTheDocument();
    expect(screen.getByText("Hierarchy")).toBeInTheDocument();
  });

  it("renders the team panel when a unit is selected", () => {
    setup({ selection: { kind: "unit", unitId: "u1" } });
    expect(screen.getByDisplayValue("Front line")).toBeInTheDocument();
  });

  it("renders the person panel when a person is selected", () => {
    setup({ selection: { kind: "person", unitId: "u1", member: { type: "agent", id: "a1" } } });
    expect(screen.getByText("Nova")).toBeInTheDocument();
  });

  it("changing autonomy calls onChange with the unit's new autonomy", async () => {
    const user = userEvent.setup();
    const { onChange } = setup({ selection: { kind: "unit", unitId: "u1" } });
    await user.click(screen.getByRole("radio", { name: "Acts alone within its limits" }));
    expect(onChange).toHaveBeenCalledTimes(1);
    const next = onChange.mock.calls[0]![0] as OrgDefinition;
    expect(next.units.find((u) => u.id === "u1")?.autonomy).toBe("auto");
  });

  it("changing the team type calls onChange", async () => {
    const user = userEvent.setup();
    const { onChange } = setup({ selection: { kind: "unit", unitId: "u1" } });
    await user.click(screen.getByLabelText("Team type"));
    await user.click(await screen.findByRole("option", { name: "Pool" }));
    const next = onChange.mock.calls[0]![0] as OrgDefinition;
    expect(next.units.find((u) => u.id === "u1")?.kind).toBe("pool");
  });

  it("changing the approval risk threshold calls onChange", async () => {
    const user = userEvent.setup();
    const { onChange } = setup({ selection: { kind: "unit", unitId: "u1" } });
    await user.click(screen.getByLabelText("Approval risk threshold"));
    await user.click(await screen.findByRole("option", { name: "High" }));
    const next = onChange.mock.calls[0]![0] as OrgDefinition;
    expect(next.units.find((u) => u.id === "u1")?.approval_risk).toBe("high");
  });

  it("changing a member's role in the team calls onChange", async () => {
    const user = userEvent.setup();
    const { onChange } = setup({ selection: { kind: "unit", unitId: "u1" } });
    const row = screen.getAllByText("Ada").map((el) => el.closest("li")).find((el): el is HTMLLIElement => el !== null)!;
    await user.click(within(row).getByLabelText("Role in the team"));
    await user.click(await screen.findByRole("option", { name: "Owner" }));
    const next = onChange.mock.calls[0]![0] as OrgDefinition;
    expect(next.units.find((u) => u.id === "u1")?.members.find((m) => m.id === "m1")?.role).toBe("owner");
  });

  it("editing a routing rule's keywords calls onChange", () => {
    const { onChange } = setup({ selection: { kind: "unit", unitId: "u1" } });
    const input = screen.getByLabelText("Keywords");
    fireEvent.change(input, { target: { value: "urgent" } });
    const next = onChange.mock.calls.at(-1)?.[0] as OrgDefinition;
    expect(next.rules[0]!.keywords).toEqual(["urgent"]);
  });

  it("distinguishes an instruction-only refusal from a tool-enforced one", () => {
    setup({ selection: { kind: "unit", unitId: "u1" } });
    // "delete" is in ORG_NON_NEGOTIABLE_DENY: always shown, no asterisk.
    const locked = screen.getByText("Delete").closest("span")!;
    expect(locked.textContent).not.toContain("*");
    // "refund" is free text the unit typed: no tool enforces it, marked with an asterisk.
    const instruction = screen.getByText(/Refund/).closest("span")!;
    expect(instruction.textContent).toContain("*");
  });

  it("says a consults link has no effect on routing", () => {
    setup({ selection: { kind: "unit", unitId: "u1" } });
    expect(screen.getByText(/consults/)).toHaveTextContent("Informational only, no effect on routing");
  });

  it("adds a committee, then puts a team in an existing one", async () => {
    const user = userEvent.setup();
    const { onChange } = setup();
    await user.click(screen.getByText("Collective decisions and market"));
    await user.click(screen.getByRole("button", { name: "Add a committee" }));
    expect((onChange.mock.calls.at(-1)?.[0] as OrgDefinition).committees).toEqual([{ decision_type: "", unit_ids: [], quorum: 1, max_rounds: 3 }]);
  });

  it("putting a team in a committee calls onChange", async () => {
    const user = userEvent.setup();
    const withCommittee = { ...definition, committees: [{ decision_type: "release", unit_ids: [], quorum: 1, max_rounds: 2 }] };
    const { onChange } = setup({ definition: withCommittee });
    await user.click(screen.getByText("Collective decisions and market"));
    await user.click(screen.getByRole("checkbox", { name: "Front line" }));
    expect((onChange.mock.calls.at(-1)?.[0] as OrgDefinition).committees[0]?.unit_ids).toEqual(["u1"]);
  });

  it("blocks editing when readOnly", () => {
    setup({ selection: { kind: "unit", unitId: "u1" }, readOnly: true });
    expect(screen.getByLabelText("Team name")).toBeDisabled();
    for (const radio of screen.getAllByRole("radio")) expect(radio).toHaveAttribute("aria-disabled", "true");
  });

  it("shows a sentence instead of nine zeroes when nothing happened", () => {
    setup();
    expect(screen.getByText("No request routed in the last 7 days. Test one to see the path it would take.")).toBeInTheDocument();
    expect(screen.queryByTestId("org-health")).toBeNull();
  });
});
