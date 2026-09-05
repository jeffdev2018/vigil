// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import type { PermissionProfile } from "@multica/core/agents/permission-profiles";
import { renderWithI18n } from "../../../test/i18n";

// Client parsing and list helpers: packages/core/agents/permission-profiles.test.ts.

const state = vi.hoisted(() => ({
  profiles: [] as PermissionProfile[],
  assign: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/agents/permission-profiles", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/agents/permission-profiles")>()),
  permissionProfilesOptions: () => ({ queryKey: ["profiles"], queryFn: async () => state.profiles }),
  useSetAgentPermissionProfile: () => ({ mutate: state.assign, isPending: false }),
}));

import { PermissionProfileField } from "./permission-profile-field";

const profile = (over: Partial<PermissionProfile> = {}): PermissionProfile => ({
  id: "p-code", name: "code", description: "Edits code.", read_only: false, denied_paths: [".env"], allowed_commands: ["*"], hidden_secrets: [], builtin: true, ...over,
});

function render(agent: Partial<Agent>, canEdit = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <PermissionProfileField agent={{ id: "a1", name: "Builder", ...agent } as Agent} canEdit={canEdit} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.profiles = [profile(), profile({ id: "p-ro", name: "read_only", read_only: true, description: "Reads only." })];
  state.assign.mockReset();
});

describe("PermissionProfileField", () => {
  it("shows the agent's profile, read-only for viewers", async () => {
    render({ permission_profile_id: "p-code" }, false);
    expect(await screen.findByText("code")).toBeTruthy();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("shows 'No profile' without one and assigns the picked profile", async () => {
    render({ permission_profile_id: null });
    fireEvent.click(await screen.findByRole("button", { name: /Permission profile: No profile/ }));
    fireEvent.click(await screen.findByText("Reads only."));
    expect(state.assign).toHaveBeenCalledWith("p-ro", expect.anything());
  });

  it("clears the profile from the picker", async () => {
    render({ permission_profile_id: "p-code" });
    fireEvent.click(await screen.findByRole("button", { name: /Permission profile: code/ }));
    fireEvent.click(await screen.findByText("No profile (full access)"));
    expect(state.assign).toHaveBeenCalledWith(null, expect.anything());
  });
});
