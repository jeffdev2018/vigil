// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, within } from "@testing-library/react";
import type { OrgOffer, OrgStructure } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({
  offers: [] as OrgOffer[],
  structure: null as OrgStructure | null,
  escalated: [] as unknown[],
  routed: [] as unknown[],
  escalateError: null as Error | null,
  toastError: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { error: state.toastError, success: vi.fn() } }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: (o: { queryKey?: readonly unknown[] }) => (o.queryKey?.[0] === "org-offers" ? { data: state.offers } : { data: state.structure }),
}));
vi.mock("@multica/core/org", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/org")>()),
  issueOrgOffersOptions: () => ({ queryKey: ["org-offers"] }),
  orgResolveOptions: () => ({ queryKey: ["org-resolve"] }),
  useEscalateIssue: () => ({ isPending: false, mutate: (v: unknown, o: { onSuccess: () => void; onError: (e: unknown) => void }) => { state.escalated.push(v); if (state.escalateError) o.onError(state.escalateError); else o.onSuccess(); } }),
  useRouteIssueNow: () => ({ isPending: false, mutate: (v: unknown, o: { onSuccess: () => void }) => { state.routed.push(v); o.onSuccess(); } }),
}));

import { IssueOrgSection } from "./issue-org-section";

const offer = (over: Partial<OrgOffer>): OrgOffer => ({
  id: "o", agent_id: "a-1", agent_name: "Codex", confidence: 0.8, cost_usd_ticks: 1_250_000, eta_hours: 4, status: "pending", created_at: "", ...over,
});
const active: OrgStructure = {
  id: "s", workspace_id: "ws-1", project_id: "p1", model: "market", name: "Market", status: "active", revision: 1, revision_id: null,
  definition: { units: [], edges: [], rules: [], committees: [], market: { price_cap_usd_ticks: 0, offers_per_agent_per_day: 0, min_offers: 0 } },
  owner_id: null, dissolve_at: null, end_condition: "", budget_usd_ticks: 0, eval_attestation: "", paused_reason: "", dissolved_at: null, paused_units: [], created_by: null, created_at: "", updated_at: "",
};

beforeEach(() => {
  state.offers = [];
  state.structure = null;
  state.escalated = [];
  state.routed = [];
  state.escalateError = null;
  state.toastError.mockReset();
});

describe("IssueOrgSection", () => {
  it("renders nothing without offers or a live structure", () => {
    state.structure = { ...active, status: "paused" };
    const { container } = renderWithI18n(<IssueOrgSection issueId="i1" issue={{ project_id: "p1", assignee_id: null }} />);
    expect(container.innerHTML).toBe("");
  });

  it("lists the offers with confidence, cost, eta and status", () => {
    state.offers = [offer({}), offer({ id: "o2", agent_name: "Claude", confidence: 0.55, cost_usd_ticks: 3_000_000, eta_hours: 2, status: "over_cap" })];
    renderWithI18n(<IssueOrgSection issueId="i1" issue={{ project_id: "p1", assignee_id: "a-1" }} />);
    const rows = screen.getAllByTestId("org-offer").map((r) => r.textContent);
    expect(rows[0]).toBe("Codex80%$1.254 hPending");
    expect(rows[1]).toBe("Claude55%$3.002 hOver cap");
    expect(screen.queryByRole("button", { name: "Escalate" })).toBeNull();
  });

  it("escalates with the reason, and routes now only when unassigned", async () => {
    state.structure = active;
    renderWithI18n(<IssueOrgSection issueId="i1" issue={{ project_id: "p1", assignee_id: null }} />);
    fireEvent.click(screen.getByRole("button", { name: "Route now" }));
    expect(state.routed).toEqual(["i1"]);
    fireEvent.click(screen.getByRole("button", { name: "Escalate" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Reason"), { target: { value: "Blocked on secrets" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Escalate" }));
    expect(state.escalated).toEqual([{ issueId: "i1", reason: "Blocked on secrets" }]);
  });

  it("surfaces the server refusal verbatim", async () => {
    state.structure = active;
    state.escalateError = new Error("escalation quota reached for today");
    renderWithI18n(<IssueOrgSection issueId="i1" issue={{ project_id: "p1", assignee_id: "a-1" }} />);
    fireEvent.click(screen.getByRole("button", { name: "Escalate" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Reason"), { target: { value: "why" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Escalate" }));
    expect(state.toastError).toHaveBeenCalledWith("escalation quota reached for today");
  });
});
