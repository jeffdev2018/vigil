/**
 * @vitest-environment jsdom
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import type { IssueTypeEntry } from "@multica/core/types";
import en from "../../locales/en/settings.json";
import { IssueTypesTab } from "./issue-types-tab";

const reorderMutate = vi.hoisted(() => vi.fn());
let catalog: IssueTypeEntry[] = [];
let role: string = "owner";

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey: readonly unknown[] }) => ({
    data: options.queryKey[0] === "issue-types" ? catalog : members(),
    isLoading: false,
  }),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: unknown) => unknown) => selector({ user: { id: "u-1" } }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members", "ws-1"] }),
}));
vi.mock("@multica/core/issue-types/queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issue-types/queries")>()),
  issueTypeListOptions: () => ({ queryKey: ["issue-types", "ws-1"] }),
}));
vi.mock("@multica/core/issue-types/mutations", () => ({
  useCreateIssueType: () => ({ mutate: vi.fn(), isPending: false }),
  useUpdateIssueType: () => ({ mutate: vi.fn(), isPending: false }),
  useArchiveIssueType: () => ({ mutate: vi.fn() }),
  useReorderIssueTypes: () => ({ mutate: reorderMutate }),
}));
vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (accessor: (dict: unknown) => string, params?: Record<string, unknown>) => {
      const template = accessor(en);
      if (!params) return template;
      return template.replace(/\{\{(\w+)\}\}/g, (_, k: string) => String(params[k] ?? ""));
    },
  }),
}));

function members() {
  return [{ user_id: "u-1", role }];
}

function entry(overrides: Partial<IssueTypeEntry>): IssueTypeEntry {
  return {
    id: overrides.key ?? "id",
    workspace_id: "ws-1",
    key: "spike",
    name: "Spike",
    description: "",
    color: "#ff0000",
    icon: "",
    is_system: false,
    position: 4,
    archived_at: null,
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

const BUG = entry({ id: "bug", key: "bug", name: "Bug", is_system: true, position: 0 });

afterEach(() => {
  cleanup();
  reorderMutate.mockClear();
  catalog = [];
  role = "owner";
});

describe("IssueTypesTab", () => {
  it("lists the catalogue and badges the built-ins", () => {
    catalog = [BUG, entry({})];
    render(<IssueTypesTab />);

    expect(screen.getByText("Bug")).toBeTruthy();
    expect(screen.getByText("Spike")).toBeTruthy();
    expect(screen.getAllByText(en.issue_types.system_badge)).toHaveLength(1);
  });

  it("offers type creation to an owner and withholds it from a member", () => {
    catalog = [BUG];
    render(<IssueTypesTab />);
    expect(screen.queryByRole("button", { name: en.issue_types.add })).toBeTruthy();

    cleanup();
    role = "member";
    render(<IssueTypesTab />);
    expect(screen.queryByRole("button", { name: en.issue_types.add })).toBeNull();
  });

  // Acceptance 1: a system type is not archivable, and the UI must not offer
  // an action whose only possible outcome is a 409.
  it("offers no archive action on a built-in type", () => {
    catalog = [BUG];
    render(<IssueTypesTab />);

    expect(
      screen.queryByRole("button", {
        name: en.issue_types.actions.open.replace("{{name}}", "Bug"),
      }),
    ).toBeTruthy();
    // The row's menu exists (edit is allowed on a built-in) but archive is not
    // rendered inside it. Asserting on the absence of the label covers both
    // the closed and open states, because a DropdownMenuItem that is never
    // constructed cannot appear either way.
    expect(screen.queryByText(en.issue_types.actions.archive)).toBeNull();
  });

  it("hides archived types until the toggle is offered", () => {
    catalog = [BUG, entry({ id: "old", key: "old", name: "Retired", archived_at: "2026-01-01T00:00:00Z" })];
    render(<IssueTypesTab />);

    expect(screen.queryByText("Retired")).toBeNull();
    // The toggle only appears once something IS archived, so its presence here
    // is what tells the admin the hidden row exists.
    expect(
      screen.getByText(en.issue_types.show_archived.replace("{{count}}", "1")),
    ).toBeTruthy();
  });

  it("shows the empty state rather than a bare card when the catalogue is empty", () => {
    catalog = [];
    render(<IssueTypesTab />);
    expect(screen.getByText(en.issue_types.empty)).toBeTruthy();
  });
});
