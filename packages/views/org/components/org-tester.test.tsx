// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import type { Goal, OrgDefinition, OrgSimulation } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Text shaping (title/description split, issue → form, escalation ladder) is
// asserted in packages/core/org/tester.test.ts. This suite keeps the wiring:
// what the panel sends, what reaches the screen, and the two failure paths.

const state = vi.hoisted(() => ({ simulateOrg: vi.fn() }));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: { simulateOrg: state.simulateOrg },
}));
vi.mock("../../modals/issue-picker-modal", () => ({
  IssuePickerModal: ({ open, onSelect }: { open: boolean; onSelect: (i: unknown) => void }) =>
    open ? (
      <button
        type="button"
        onClick={() =>
          onSelect({
            id: "i-1",
            title: "Payment webhook retries forever",
            description: "Stripe answers 500 and we never stop.",
            labels: [{ name: "billing" }],
          })
        }
      >
        Pick MUL-2
      </button>
    ) : null,
}));

import { OrgTester } from "./org-tester";

const definition: OrgDefinition = {
  units: [
    { id: "support", name: "Support", mission_goal_id: "g-1", excludes: [], autonomy: "draft", allow: [], deny: [], escalation_quota_per_day: 3, members: [], roles: [] },
    { id: "lead", name: "Lead", excludes: [], autonomy: "auto", allow: [], deny: [], escalation_quota_per_day: 3, members: [], roles: [] },
  ],
  edges: [{ from: "support", to: "lead", kind: "reports_to" }],
  rules: [],
  committees: [],
  market: { price_cap_usd_ticks: 0, offers_per_agent_per_day: 0, min_offers: 0 },
};

const goals: Goal[] = [
  {
    id: "g-1", workspace_id: "ws-1", parent_goal_id: null, title: "Answer every customer in a day", description: "",
    success_measure: "", due_date: null, owner_id: null, status: "active", created_at: "", updated_at: "",
    issue_count: 0, done_count: 0, project_ids: [],
  },
];

const simulation = (over: Partial<OrgSimulation> = {}): OrgSimulation => ({
  basis: "draft",
  structure_id: "s-1",
  revision: 4,
  unit: { id: "support", name: "Support", model: "hierarchy", autonomy: "draft" },
  receives: { unit_id: "support", unit_name: "Support" },
  prepares: { kind: "agent", id: "a-1", name: "Mika" },
  decides: { kind: "member", id: "u-1", name: "Ada" },
  escalation_path: [{ unit_id: "lead", unit_name: "Lead" }],
  blocking_denies: [],
  cost_estimate_usd_ticks: 15_000_000_000,
  notes: [],
  ...over,
});

function render(props: Partial<React.ComponentProps<typeof OrgTester>> = {}) {
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <OrgTester
        structureId="s-1"
        definition={definition}
        model="hierarchy"
        status="draft"
        revision={4}
        dirty={false}
        goals={goals}
        onSelectUnit={() => {}}
        {...props}
      />
    </QueryClientProvider>,
  );
}

const type = (value: string) => fireEvent.change(screen.getByLabelText("The request"), { target: { value } });
const run = () => fireEvent.click(screen.getByRole("button", { name: "Test" }));

beforeEach(() => {
  state.simulateOrg.mockReset();
});

describe("OrgTester", () => {
  it("hides the previous routing after the request changes", async () => {
    state.simulateOrg.mockResolvedValue(simulation());
    render(); type("Help with billing"); run();
    await waitFor(() => expect(screen.getByTestId("org-tester-result")).toBeVisible());
    type("A different request");
    expect(screen.queryByTestId("org-tester-result")).toBeNull();
    expect(screen.getByRole("status").textContent).toContain("Run the test again");
  });

  it("shows the invitation and no result until a simulation ran", () => {
    render();
    expect(screen.getByTestId("org-tester-empty")).toBeTruthy();
    expect(screen.queryByTestId("org-tester-result")).toBeNull();
    expect(screen.getByRole("button", { name: "Test" })).toBeDisabled();
  });

  it("simulates the canvas definition and renders the five work lines, the cost and the notes", async () => {
    state.simulateOrg.mockResolvedValue(simulation({ notes: ["no spend observed for this unit over the last 30 days"] }));
    render();
    type("Card declined at checkout\nThree customers today.");
    run();

    await screen.findByTestId("org-tester-result");
    expect(state.simulateOrg).toHaveBeenCalledWith({
      model: "hierarchy",
      definition,
      structure_id: "s-1",
      request: { title: "Card declined at checkout", description: "Three customers today.", labels: [] },
    });
    expect(screen.getByTestId("org-tester-receives").textContent).toContain("Support");
    expect(screen.getByTestId("org-tester-receives").textContent).toContain("Answer every customer in a day");
    expect(screen.getByTestId("org-tester-prepares").textContent).toContain("Mika · agent");
    expect(screen.getByTestId("org-tester-prepares").textContent).toContain("Prepares drafts");
    expect(screen.getByTestId("org-tester-decides").textContent).toContain("Ada · member");
    expect(screen.getByTestId("org-tester-escalates").textContent).toContain("Lead");
    expect(screen.getByTestId("org-tester-blocked").textContent).toContain("Nothing this unit refuses.");
    expect(screen.getByTestId("org-tester-cost").textContent).toContain("$1.50");
    expect(screen.getByTestId("org-tester-notes").textContent).toContain("no spend observed");
  });

  it("says a root unit escalates nowhere rather than showing an empty line", async () => {
    state.simulateOrg.mockResolvedValue(simulation({ escalation_path: [] }));
    render();
    type("Anything");
    run();
    await screen.findByTestId("org-tester-result");
    expect(screen.getByTestId("org-tester-escalates").textContent).toContain("— (root)");
  });

  it("names the refusals that block the request and what happens instead", async () => {
    state.simulateOrg.mockResolvedValue(simulation({ blocking_denies: ["refund", "deploy"] }));
    render();
    type("Refund the order and deploy the fix");
    run();
    await screen.findByTestId("org-tester-result");
    const blocked = screen.getByTestId("org-tester-blocked").textContent ?? "";
    expect(blocked).toContain("refund");
    expect(blocked).toContain("deploy");
    expect(blocked).toContain("the request will be escalated with its context; this unit will not carry it out.");
  });

  it("selects the receiving unit in the canvas from the result", async () => {
    state.simulateOrg.mockResolvedValue(simulation());
    const onSelectUnit = vi.fn();
    render({ onSelectUnit });
    type("Anything");
    run();
    fireEvent.click(await screen.findByTestId("org-tester-unit-link"));
    expect(onSelectUnit).toHaveBeenCalledWith("support");
  });

  it("says the basis is the unsaved draft, the saved draft, or the active revision", () => {
    const unsaved = render({ dirty: true });
    expect(screen.getByTestId("org-tester").textContent).toContain("Against the draft, rev. 4 (unsaved).");
    unsaved.unmount();

    const draft = render();
    expect(screen.getByTestId("org-tester").textContent).toContain("Against the draft, rev. 4.");
    draft.unmount();

    render({ status: "active", revision: 9 });
    expect(screen.getByTestId("org-tester").textContent).toContain("Against the active revision, rev. 9.");
  });

  it("sends the invalid definition back to the canvas problems on a 422", async () => {
    state.simulateOrg.mockRejectedValue(new ApiError("a hierarchy has exactly one root", 422, "Unprocessable Entity"));
    render();
    type("Anything");
    run();
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("a hierarchy has exactly one root");
    expect(alert.textContent).toContain("Settle the problems listed beside the chart first.");
    expect(screen.getByTestId("org-tester-empty")).toBeTruthy();
  });

  it("fills the request from a real issue, labels apart", async () => {
    state.simulateOrg.mockResolvedValue(simulation());
    render();
    fireEvent.click(screen.getByRole("button", { name: "Test with a real issue" }));
    fireEvent.click(screen.getByRole("button", { name: "Pick MUL-2" }));

    const field = screen.getByLabelText("The request") as HTMLTextAreaElement;
    expect(field.value).toBe("Payment webhook retries forever\nStripe answers 500 and we never stop.");
    expect(screen.getByTestId("org-tester-labels").textContent).toContain("billing");

    run();
    await waitFor(() =>
      expect(state.simulateOrg).toHaveBeenCalledWith(
        expect.objectContaining({
          request: { title: "Payment webhook retries forever", description: "Stripe answers 500 and we never stop.", labels: ["billing"] },
        }),
      ),
    );
  });

  it("fills the request from a clickable example", () => {
    render();
    fireEvent.click(screen.getAllByTestId("org-tester-example")[0]!);
    expect((screen.getByLabelText("The request") as HTMLTextAreaElement).value).toBe(
      "My card was declined at checkout, three customers today already.",
    );
  });
});
