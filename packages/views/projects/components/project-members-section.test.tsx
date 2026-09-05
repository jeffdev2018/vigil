// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, within } from "@testing-library/react";
import type { ProjectMemberRole } from "@multica/core/access";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({
  members: [] as unknown[],
  role: "member" as "owner" | "admin" | "member",
  set: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: { user: { id: string } }) => unknown) => selector({ user: { id: "user-1" } }),
}));
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ role: state.role, isLoading: false }),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: { members: state.members, roles: ["viewer", "contributor", "admin"] } }),
}));
vi.mock("@multica/core/access", () => ({
  projectMembersOptions: () => ({ queryKey: ["project", "p1", "ws-1", "members"] }),
  useSetProjectMemberRole: () => ({ isPending: false, mutate: state.set }),
}));

import { ProjectMembersSection } from "./project-members-section";

const member = (over: Partial<ProjectMemberRole>): ProjectMemberRole => ({
  subject_type: "member", subject_id: "user-2", name: "Bob", email: "bob@example.com", workspace_role: "member",
  ceiling: "contributor", effective_role: "contributor", source: "inherited", override: null, ...over,
});

const render = () => renderWithI18n(<ProjectMembersSection projectId="p1" />);

beforeEach(() => {
  state.members = [];
  state.role = "member";
  state.set.mockReset();
});

describe("ProjectMembersSection", () => {
  it("never offers a role above the ceiling", () => {
    state.role = "admin";
    state.members = [member({})];
    render();
    const select = screen.getByRole("combobox", { name: "Project role" });
    const values = within(select).getAllByRole("option").map((o) => (o as HTMLOptionElement).value);
    expect(values).toEqual(["__inherit", "viewer", "contributor"]);
    expect(screen.getByRole("option", { name: "Inherit (contributor)" })).toBeInTheDocument();
  });

  it("marks inherited and overridden roles", () => {
    state.members = [member({}), member({ subject_id: "user-3", name: "Cara", source: "override", override: "viewer", effective_role: "viewer" })];
    render();
    const rows = screen.getAllByTestId("project-member-row");
    expect(rows[0]).toHaveTextContent("inherited");
    expect(rows[1]).toHaveTextContent("override");
    expect(rows[1]).toHaveTextContent("viewer");
    // A plain member with no project admin role sees no select.
    expect(screen.queryByRole("combobox")).toBeNull();
    expect(screen.getByText(/Only workspace owners, admins and project admins/)).toBeInTheDocument();
  });

  it("sets an override and clears it back to inherited", () => {
    // The current user is a project admin through their own row.
    state.members = [member({ subject_id: "user-1", name: "Me", ceiling: "admin", effective_role: "admin" }), member({})];
    render();
    const selects = screen.getAllByRole("combobox", { name: "Project role" });
    fireEvent.change(selects[1]!, { target: { value: "viewer" } });
    expect(state.set).toHaveBeenLastCalledWith({ subjectType: "member", subjectId: "user-2", role: "viewer" }, expect.anything());
    fireEvent.change(selects[1]!, { target: { value: "__inherit" } });
    expect(state.set).toHaveBeenLastCalledWith({ subjectType: "member", subjectId: "user-2", role: null }, expect.anything());
  });

  it("labels agents", () => {
    state.members = [member({ subject_type: "agent", subject_id: "agent-1", name: "Reviewer", email: undefined })];
    render();
    expect(screen.getByTestId("project-member-row")).toHaveTextContent("agent");
  });
});
