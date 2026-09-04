// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { BusinessRule, Workspace } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Client parsing: packages/core/workspace/business-rules.test.ts.

const state = vi.hoisted(() => ({
  rules: [] as BusinessRule[],
  created: [] as unknown[],
  statuses: [] as unknown[],
}));

const draft: BusinessRule = {
  id: "r1", workspace_id: "ws-1", title: "Three projects at most", natural_language: "at most three projects", predicate: {},
  description: "the number of projects in the workspace must be at most 3", attach_point: "project_create", status: "draft",
  created_by: "u1", created_at: "", updated_at: "",
};

vi.mock("@multica/core/workspace/business-rules", () => ({
  businessRulesOptions: (wsId: string) => ({ queryKey: ["business-rules", wsId], queryFn: async () => ({ rules: state.rules, attach_points: ["project_create", "issue_submit_review"] }) }),
  useCreateBusinessRule: () => ({
    isPending: false,
    mutate: (v: unknown, o: { onSuccess: (r: BusinessRule) => void }) => {
      state.created.push(v);
      o.onSuccess(draft);
    },
  }),
  useDryRunBusinessRule: () => ({
    isPending: false,
    mutate: (_id: string, o: { onSuccess: (d: unknown) => void }) =>
      o.onSuccess({ rule: draft, checked: 1, violations: [{ subject_type: "project_create", subject_id: "ws-1", label: "the next project", detail: "observed 4" }] }),
  }),
  useSetBusinessRuleStatus: () => ({
    isPending: false,
    mutate: (v: unknown, o?: { onSuccess?: () => void }) => {
      state.statuses.push(v);
      o?.onSuccess?.();
    },
  }),
  useDeleteBusinessRule: () => ({ mutate: vi.fn() }),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { BusinessRulesSetting } from "./business-rules-setting";

const workspace = { id: "ws-1", settings: {} } as unknown as Workspace;

function render(canEdit = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <BusinessRulesSetting workspace={workspace} canEdit={canEdit} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.rules = [];
  state.created = [];
  state.statuses = [];
});

describe("BusinessRulesSetting", () => {
  it("previews a rule in plain words with its dry-run, and activates only from the preview", async () => {
    render();
    expect(await screen.findByTestId("rules-empty")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Rule"), { target: { value: "A workspace has at most three projects" } });
    fireEvent.change(screen.getByLabelText("Applies when"), { target: { value: "project_create" } });
    fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    expect(state.created[0]).toEqual({ natural_language: "A workspace has at most three projects", attach_point: "project_create" });
    const preview = await screen.findByTestId("rule-preview");
    expect(preview.textContent).toContain("must be at most 3");
    expect(preview.textContent).toContain("the next project");
    fireEvent.click(screen.getByRole("button", { name: "Activate" }));
    expect(state.statuses[0]).toEqual({ id: "r1", status: "active" });
  });

  it("lists rules with their status and lets an admin disable an active one", async () => {
    state.rules = [{ ...draft, status: "active" }, { ...draft, id: "r2", status: "disabled", title: "Old rule" }];
    render();
    const rows = await screen.findAllByTestId("business-rule");
    expect(rows).toHaveLength(2);
    expect(rows[0]?.getAttribute("data-status")).toBe("active");
    fireEvent.click(screen.getAllByRole("button", { name: "Disable" })[0]!);
    expect(state.statuses[0]).toEqual({ id: "r1", status: "disabled" });
    expect(screen.getAllByRole("button", { name: "Activate" })).toHaveLength(1);
  });

  it("hides the editor from members who cannot manage the workspace", async () => {
    state.rules = [draft];
    render(false);
    await screen.findAllByTestId("business-rule");
    expect(screen.queryByLabelText("Rule")).toBeNull();
    expect(screen.queryByRole("button", { name: "Activate" })).toBeNull();
  });
});
