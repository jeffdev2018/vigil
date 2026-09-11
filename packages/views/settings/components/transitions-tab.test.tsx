// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { IssueTransitionRule } from "@multica/core/issue-transitions";
import { renderWithI18n } from "../../test/i18n";

// Parsing lives in packages/core/issue-transitions/schemas.test.ts.

const state = vi.hoisted(() => ({
  rules: [] as IssueTransitionRule[],
  role: "member" as string,
  remove: vi.fn(),
  rulesError: null as Error | null,
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/auth", () => ({ useAuthStore: (sel: (s: unknown) => unknown) => sel({ user: { id: "u1" } }) }));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: async () => [{ user_id: "u1", role: state.role }] }),
}));
vi.mock("@multica/core/issue-transitions", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issue-transitions")>()),
  issueTransitionRulesOptions: () => ({
    queryKey: ["rules"],
    queryFn: async () => {
      if (state.rulesError) throw state.rulesError;
      return { rules: state.rules, categories: [] };
    },
  }),
  useSaveIssueTransitionRule: () => ({ mutate: vi.fn(), isPending: false }),
  useDeleteIssueTransitionRule: () => ({ mutate: state.remove, isPending: false }),
}));

import { TransitionsTab } from "./transitions-tab";

const rule = (over: Partial<IssueTransitionRule> = {}): IssueTransitionRule => ({
  id: "r1",
  workspace_id: "ws-1",
  project_id: null,
  from_category: "in_progress",
  to_category: "done",
  allowed_roles: ["admin"],
  allow_actor_types: [],
  requires_approval: false,
  approver_roles: [],
  reject_status_key: null,
  enabled: true,
  actors: [],
  created_at: "",
  updated_at: "",
  ...over,
});

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <TransitionsTab />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.rules = [];
  state.role = "admin";
  state.remove.mockReset();
  state.rulesError = null;
});

describe("TransitionsTab", () => {
  // A failed fetch must not read as "no rules configured" — that would tell
  // an admin every transition is free when the truth is unknown.
  it("reports a load failure instead of the free-transitions empty state", async () => {
    state.rulesError = new Error("network down");
    render();
    expect(await screen.findByRole("alert")).toHaveTextContent(/could not load/i);
    expect(screen.queryByText(/Every transition is free/)).toBeNull();

    state.rulesError = null;
    state.rules = [rule()];
    (await screen.findByRole("button", { name: /retry/i })).click();
    expect(await screen.findByText("in_progress → done")).toBeTruthy();
  });

  it("says out loud that no rules means free transitions", async () => {
    // The empty state is the whole explanation of the feature for an admin who
    // arrives here wondering why the picker greys nothing.
    render();
    expect(await screen.findByText(/Every transition is free/)).toBeTruthy();
  });

  it("summarises a rule as origin, target and who it grants", async () => {
    state.rules = [rule({ requires_approval: true })];
    render();
    expect(await screen.findByText("In Progress → Done")).toBeTruthy();
    const summary = await screen.findByText(/Allowed: Admin/);
    expect(summary.textContent).toContain("needs approval");
  });

  it("reads any-origin as a named origin rather than an empty gap", async () => {
    state.rules = [rule({ from_category: null })];
    render();
    expect(await screen.findByText("any status → Done")).toBeTruthy();
  });

  it("hides every write affordance from a plain member", async () => {
    state.rules = [rule()];
    state.role = "member";
    render();
    await screen.findByText("In Progress → Done");
    expect(screen.queryByRole("button", { name: "Add rule" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Edit rule" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Delete rule" })).toBeNull();
  });

  it("offers the write affordances to an admin", async () => {
    state.rules = [rule()];
    state.role = "owner";
    render();
    await waitFor(() => expect(screen.getByRole("button", { name: "Add rule" })).toBeTruthy());
    expect(screen.getByRole("button", { name: "Edit rule" })).toBeTruthy();
  });
});
