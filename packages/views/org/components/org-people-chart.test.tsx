// @vitest-environment jsdom
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen, within } from "@testing-library/react";
import { validateOrgDefinition } from "@multica/core/org/validate";
import type { Agent, MemberWithUser, OrgDefinition, OrgUnit } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Who reports to whom, and the tree geometry, are asserted in
// packages/core/org/people.test.ts. This suite keeps what reaches the screen:
// the card, the record, zoom, and that adding a teammate writes the definition.

vi.mock("@multica/core/api", () => ({ api: {} }));
vi.mock("@multica/core/runtimes/queries", () => ({ runtimeListOptions: () => ({ queryKey: ["runtimes"] }) }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: [{ id: "rt-1", name: "Claude Code", custom_name: "", provider: "claude" }], isLoading: false }),
}));
vi.mock("@multica/core/agents", () => ({
  useWorkspacePresenceMap: () => ({
    byAgent: new Map([["nova", { availability: "online", workload: "working", queuedCount: 0 }]]),
    loading: false,
  }),
}));

import { OrgPeopleChart } from "./org-people-chart";

const unit = (over: Partial<OrgUnit>): OrgUnit => ({
  id: "u", name: "Unit", excludes: ["external_effects"], autonomy: "draft", allow: [], deny: [],
  escalation_quota_per_day: 5, members: [], roles: [], ...over,
});

const definition: OrgDefinition = {
  units: [
    unit({ id: "dir", name: "Direction", owner_id: "jeff", members: [{ type: "member", id: "jeff", role: "Owner" }] }),
    unit({ id: "support", name: "Support", owner_id: "lea", members: [{ type: "member", id: "lea", role: "Support lead" }, { type: "agent", id: "nova", role: "Support L1" }, { type: "agent", id: "vera" }] }),
  ],
  edges: [{ from: "support", to: "dir", kind: "reports_to" }],
  rules: [],
  committees: [],
  market: { price_cap_usd_ticks: 0, offers_per_agent_per_day: 0, min_offers: 0 },
};

const members = [
  { user_id: "jeff", name: "Jeff", avatar_url: null, role: "owner" },
  { user_id: "lea", name: "Léa", avatar_url: null, role: "member" },
] as unknown as MemberWithUser[];
const agents = [
  { id: "nova", name: "Nova", avatar_url: null, runtime_id: "rt-1" },
  { id: "vera", name: "Vera", avatar_url: null, runtime_id: "" },
  { id: "kimi", name: "Kimi", avatar_url: null, runtime_id: "rt-1" },
] as unknown as Agent[];

function Harness({ initial = definition, onChange }: { initial?: OrgDefinition; onChange?: (d: OrgDefinition) => void }) {
  const [def, setDef] = useState(initial);
  const [selectedUnitId, setSelectedUnitId] = useState<string | null>(null);
  return (
    <OrgPeopleChart
      wsId="ws-1"
      definition={def}
      pausedUnits={[]}
      problems={validateOrgDefinition(def, { model: "hierarchy" })}
      members={members}
      agents={agents}
      goals={[]}
      readOnly={false}
      onChange={(next) => {
        setDef(next);
        onChange?.(next);
      }}
      undoDepth={0}
      onUndo={() => {}}
      selectedUnitId={selectedUnitId}
      onSelectUnit={setSelectedUnitId}
    />
  );
}

describe("OrgPeopleChart", () => {
  it("draws one card per person with title and runtime, and a line per report", () => {
    renderWithI18n(<Harness />);
    const cards = screen.getAllByTestId("org-person");
    expect(cards.map((c) => c.textContent)).toEqual([
      expect.stringContaining("JeffOwner · human"),
      expect.stringContaining("LéaSupport lead · human"),
      expect.stringContaining("NovaSupport L1 · Claude Code"),
      expect.stringContaining("VeraSupport · agent"),
    ]);
    // Only the agent with a running task wears the badge.
    expect(cards[2]?.textContent).toContain("Active");
    expect(cards[3]?.textContent).not.toContain("Active");
    expect(screen.getAllByTestId("org-person-edge")).toHaveLength(3);
  });

  it("opens the record with the manager and the team, and folds the unit sheet under Advanced", () => {
    renderWithI18n(<Harness />);
    fireEvent.click(screen.getByText("Léa"));
    const sheet = screen.getByTestId("org-person-sheet");
    expect(sheet.textContent).toContain("Reports to");
    expect(within(sheet).getByRole("button", { name: "Jeff" })).toBeTruthy();
    expect(within(sheet).getByRole("button", { name: "Nova" })).toBeTruthy();
    expect(within(sheet).getByRole("button", { name: "Vera" })).toBeTruthy();
    expect(sheet.textContent).toContain("Support");
    expect(within(sheet).getByTestId("org-unit-sheet")).toBeTruthy();
    // Walking the chart from the record moves the selection.
    fireEvent.click(within(sheet).getByRole("button", { name: "Jeff" }));
    expect(screen.getByTestId("org-person-sheet").textContent).toContain("Nobody — head of the chart");
  });

  it("zooms the chart without touching the definition", () => {
    renderWithI18n(<Harness />);
    const stage = () => screen.getByTestId("org-people-chart").querySelector<HTMLElement>("[style*='scale']")!;
    expect(stage().style.transform).toBe("scale(1)");
    fireEvent.click(screen.getByRole("button", { name: "Zoom out" }));
    expect(stage().style.transform).toBe("scale(0.85)");
    fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));
    expect(stage().style.transform).toBe("scale(1)");
  });

  it("adds a teammate under the chosen manager and selects the new card", () => {
    const onChange = vi.fn();
    renderWithI18n(<Harness onChange={onChange} />);
    fireEvent.click(screen.getByRole("button", { name: "Add a teammate" }));
    const dialog = screen.getByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Who"), { target: { value: "kimi" } });
    fireEvent.change(within(dialog).getByLabelText("Title"), { target: { value: "Support L2" } });
    fireEvent.change(within(dialog).getByLabelText("Reports to"), { target: { value: "support/member:lea" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Add" }));
    const next = onChange.mock.calls[0]?.[0] as OrgDefinition;
    expect(next.units.find((u) => u.id === "support")?.members.at(-1)).toEqual({ type: "agent", id: "kimi", role: "Support L2" });
    expect(screen.getAllByTestId("org-person")).toHaveLength(5);
    expect(screen.getByTestId("org-person-sheet").textContent).toContain("Kimi");
  });
});
