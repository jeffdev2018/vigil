// @vitest-environment jsdom
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen, within } from "@testing-library/react";
import { validateOrgDefinition } from "@multica/core/org/validate";
import type { MemberWithUser, OrgDefinition, OrgModel, OrgUnit } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Layout geometry and the validation matrix are asserted in
// packages/core/org/layout.test.ts and packages/core/org/validate.test.ts. This
// suite keeps the wiring: what reaches the screen, the keyboard path, and that a
// gesture writes back into the definition.

vi.mock("@multica/core/api", () => ({ api: {} }));
vi.mock("../../editor/mermaid-diagram", () => ({ MermaidDiagram: ({ chart }: { chart: string }) => <pre data-testid="mermaid">{chart}</pre> }));

import { OrgCanvas } from "./org-canvas";

const unit = (over: Partial<OrgUnit>): OrgUnit => ({
  id: "u", name: "Unit", excludes: ["external_effects"], autonomy: "draft", allow: [], deny: [],
  escalation_quota_per_day: 5, members: [], roles: [], ...over,
});

const definition: OrgDefinition = {
  units: [
    unit({ id: "lead", name: "Lead", members: [{ type: "member", id: "u-1" }] }),
    unit({ id: "dev", name: "Dev", members: [{ type: "agent", id: "a-1" }] }),
  ],
  edges: [
    { from: "dev", to: "lead", kind: "reports_to" },
    { from: "dev", to: "lead", kind: "escalates_to" },
  ],
  rules: [],
  committees: [],
  market: { price_cap_usd_ticks: 0, offers_per_agent_per_day: 0, min_offers: 0 },
};

const members: MemberWithUser[] = [
  { id: "m-1", workspace_id: "ws-1", user_id: "u-1", role: "owner", created_at: "", name: "Ada", email: "a@b.c", avatar_url: null },
];
const agents = [{ id: "a-1", name: "Mika", avatar_url: null }];

/** Mirrors what OrgPage wires: the canvas is controlled, and the problems it
 *  paints are recomputed from the definition it just handed back. */
function Harness({
  initial = definition,
  model = "hierarchy" as OrgModel,
  agentTrust,
  onChange,
}: {
  initial?: OrgDefinition;
  model?: OrgModel;
  agentTrust?: Record<string, string>;
  onChange?: (d: OrgDefinition) => void;
}) {
  const [def, setDef] = useState(initial);
  return (
    <OrgCanvas
      definition={def}
      model={model}
      pausedUnits={[]}
      problems={validateOrgDefinition(def, { model, agentTrust, agentName: { "a-1": "Mika" } })}
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
    />
  );
}

describe("OrgCanvas", () => {
  it("draws a hierarchy as cards on levels, with a solid reports_to and a dashed escalates_to", () => {
    renderWithI18n(<Harness />);
    expect(screen.getAllByTestId("org-unit-card").map((c) => c.getAttribute("data-unit-id"))).toEqual(["lead", "dev"]);
    const edges = screen.getAllByTestId("org-edge");
    expect(edges.map((e) => e.getAttribute("data-kind"))).toEqual(["reports_to", "escalates_to"]);
    expect(edges[0]?.getAttribute("stroke-dasharray")).toBeNull();
    expect(edges[1]?.getAttribute("stroke-dasharray")).toBe("4 3");
    // Members ride the cards as avatars, named for the directory, not by id.
    expect(screen.getAllByTestId("org-member-chip").map((c) => c.getAttribute("title"))).toEqual(["Ada", "Mika"]);
    expect(screen.queryByTestId("mermaid")).toBeNull();
  });

  it("counts past six members instead of stacking avatars", () => {
    const crowded = {
      ...definition,
      units: [unit({ id: "lead", name: "Lead", members: Array.from({ length: 9 }, (_, i) => ({ type: "member" as const, id: `m${i}` })) })],
      edges: [],
    };
    renderWithI18n(<Harness initial={crowded} />);
    expect(screen.getAllByTestId("org-member-chip")).toHaveLength(6);
    expect(screen.getByText("+3")).toBeTruthy();
  });

  it("moves a member to another unit from the record's list, with no pointer involved", () => {
    const changes: OrgDefinition[] = [];
    renderWithI18n(<Harness onChange={(d) => changes.push(d)} />);
    fireEvent.click(screen.getAllByTestId("org-unit-card")[1]!.querySelector("[data-org-card]")!);
    const sheet = screen.getByTestId("org-unit-sheet");
    fireEvent.change(within(sheet).getByLabelText("Move Mika to another unit"), { target: { value: "lead" } });
    const next = changes.at(-1)!;
    expect(next.units.find((u) => u.id === "dev")?.members).toEqual([]);
    expect(next.units.find((u) => u.id === "lead")?.members).toHaveLength(2);
  });

  it("walks the cards with the arrow keys and opens the focused one", () => {
    renderWithI18n(<Harness />);
    const cards = screen.getAllByTestId("org-unit-card").map((c) => c.querySelector<HTMLElement>("[data-org-card]")!);
    cards[0]!.focus();
    fireEvent.keyDown(cards[0]!, { key: "ArrowDown" });
    expect(document.activeElement).toBe(cards[1]);
    fireEvent.click(cards[1]!);
    expect(within(screen.getByTestId("org-unit-sheet")).getByLabelText("Name")).toHaveValue("Dev");
  });

  it("warns about the Rule of Two as soon as the unit is made to handle everything", () => {
    renderWithI18n(<Harness />);
    fireEvent.click(screen.getAllByTestId("org-unit-card")[0]!.querySelector("[data-org-card]")!);
    const sheet = screen.getByTestId("org-unit-sheet");
    expect(within(sheet).queryByText(/human approval will be required/)).toBeNull();
    fireEvent.click(within(sheet).getByLabelText("External effects"));
    expect(screen.getByTestId("org-unit-sheet").textContent).toContain("a human approval will be required");
    // The card carries the same signal, so the chart shows where the problem is.
    expect(screen.getAllByTestId("org-card-problem").length).toBeGreaterThan(0);
  });

  it("says when the dial grants an agent more than its own trust mode", () => {
    renderWithI18n(<Harness agentTrust={{ "a-1": "approval" }} />);
    fireEvent.click(screen.getAllByTestId("org-unit-card")[1]!.querySelector("[data-org-card]")!);
    fireEvent.click(screen.getByTestId("org-trust-auto"));
    expect(screen.getByTestId("org-unit-sheet").textContent).toContain("Mika's trust dial stops at approval");
  });

  it("keeps the diagram for the models that have no layout of their own yet", () => {
    renderWithI18n(<Harness model="circles" />);
    expect(screen.queryByTestId("org-unit-card")).toBeNull();
    expect(screen.getByTestId("mermaid").textContent).toContain("graph TD");
  });
});
