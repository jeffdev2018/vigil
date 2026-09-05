// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { PermissionProfile } from "@multica/core/agents/permission-profiles";
import { renderWithI18n } from "../../test/i18n";

// Client parsing and list helpers: packages/core/agents/permission-profiles.test.ts.

const state = vi.hoisted(() => ({
  profiles: [] as PermissionProfile[],
  update: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/agents/permission-profiles", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/agents/permission-profiles")>()),
  permissionProfilesOptions: () => ({ queryKey: ["profiles"], queryFn: async () => state.profiles }),
  useUpdatePermissionProfile: () => ({ mutate: state.update, isPending: false }),
}));

import { PermissionProfilesSetting } from "./permission-profiles-setting";

function render(canEdit = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <PermissionProfilesSetting canEdit={canEdit} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.profiles = [
    { id: "p1", name: "code", description: "Edits code.", read_only: false, denied_paths: [".env", "infra/**"], allowed_commands: ["*"], hidden_secrets: ["*PROD*"], builtin: true },
    { id: "p2", name: "read_only", description: "Reads.", read_only: true, denied_paths: [], allowed_commands: ["git status"], hidden_secrets: ["*"], builtin: true },
  ];
  state.update.mockReset();
});

describe("PermissionProfilesSetting", () => {
  it("lists every profile with its rules and saves an edited list on blur", async () => {
    render();
    expect(await screen.findByText("code")).toBeTruthy();
    expect(screen.getAllByTestId("permission-profile-card")).toHaveLength(2);
    const denied = screen.getByLabelText("code: Denied paths") as HTMLInputElement;
    expect(denied.value).toBe(".env, infra/**");
    fireEvent.change(denied, { target: { value: ".env, infra/**, deploy/**" } });
    fireEvent.blur(denied);
    expect(state.update).toHaveBeenCalledWith({ id: "p1", patch: { denied_paths: [".env", "infra/**", "deploy/**"] } }, expect.anything());
    // An unchanged value does not save.
    fireEvent.blur(screen.getByLabelText("read_only: Hidden secrets"));
    expect(state.update).toHaveBeenCalledTimes(1);
  });

  it("toggles read-only", async () => {
    render();
    fireEvent.click(await screen.findByLabelText("code: Read-only"));
    expect(state.update).toHaveBeenCalledWith({ id: "p1", patch: { read_only: true } }, expect.anything());
  });

  it("stays inert for viewers", async () => {
    render(false);
    const denied = (await screen.findByLabelText("code: Denied paths")) as HTMLInputElement;
    expect(denied.disabled).toBe(true);
    const toggle = screen.getByLabelText("code: Read-only");
    expect(toggle.hasAttribute("data-disabled") || toggle.getAttribute("aria-disabled") === "true" || (toggle as HTMLButtonElement).disabled).toBe(true);
  });
});
