// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../../test/i18n";

// Data residency (K46). The token normalization matrix lives beside the helper
// in packages/core/residency/schemas.test.ts; this suite covers the two states
// the reader must be able to tell apart — no constraint vs a live policy — plus
// the wiring that turns a typed chip into a saved policy.

const state = vi.hoisted(() => ({
  save: vi.fn(),
  policy: {
    region_allowlist: [] as string[],
    banned_providers: [] as string[],
    require_on_prem: false,
    max_list_length: 20,
  },
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/residency", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/residency")>()),
  dataResidencyOptions: () => ({
    queryKey: ["data-residency", JSON.stringify(state.policy)],
    queryFn: async () => state.policy,
  }),
  useSaveDataResidencyPolicy: () => ({ mutate: state.save, isPending: false }),
}));

import { DataResidencySetting } from "./data-residency-setting";

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <DataResidencySetting canEdit />
    </QueryClientProvider>,
  );
}

describe("DataResidencySetting", () => {
  it("reads as a neutral empty state, not a warning, when nothing is constrained", async () => {
    state.save.mockClear();
    state.policy = { region_allowlist: [], banned_providers: [], require_on_prem: false, max_list_length: 20 };
    render();
    expect(
      await screen.findByText("No constraint: every runtime in this workspace is eligible."),
    ).toBeTruthy();
    // The unverified-declaration warning belongs to a live policy: showing it
    // with no policy would warn about something that cannot happen yet.
    expect(screen.queryByText(/never verified/)).toBeNull();
  });

  it("warns that declarations are unverified once a policy is live", async () => {
    state.save.mockClear();
    state.policy = {
      region_allowlist: ["eu-west-1"],
      banned_providers: [],
      require_on_prem: false,
      max_list_length: 20,
    };
    render();
    expect(await screen.findByText(/never verified/)).toBeTruthy();
    expect(screen.queryByText("No constraint: every runtime in this workspace is eligible.")).toBeNull();
    // The stored region renders as a chip that can be removed.
    expect(screen.getByText("eu-west-1")).toBeTruthy();
    fireEvent.click(screen.getByLabelText("Remove eu-west-1"));
    expect(state.save).toHaveBeenCalledWith(
      { region_allowlist: [], banned_providers: [], require_on_prem: false },
      expect.anything(),
    );
  });

  it("normalizes a typed region before saving the whole policy", async () => {
    state.save.mockClear();
    state.policy = {
      region_allowlist: [],
      banned_providers: ["codex"],
      require_on_prem: false,
      max_list_length: 20,
    };
    render();
    // Wait for the stored policy to land: the input renders before the query
    // resolves, and saving from an empty draft would silently drop the
    // untouched lists this assertion is about.
    await screen.findByText("codex");
    const input = screen.getByLabelText("Allowed regions") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "  EU-West-1 " } });
    fireEvent.keyDown(input, { key: "Enter" });
    // Trimmed and lowercased locally so the chip matches what the server
    // stores, and the untouched lists travel with it: PUT replaces the object.
    expect(state.save).toHaveBeenCalledWith(
      { region_allowlist: ["eu-west-1"], banned_providers: ["codex"], require_on_prem: false },
      expect.anything(),
    );
    await waitFor(() => expect(input.value).toBe(""));
  });

  it("saves the on-prem requirement as part of the whole policy", async () => {
    state.save.mockClear();
    state.policy = {
      region_allowlist: ["eu-west-1"],
      banned_providers: [],
      require_on_prem: false,
      max_list_length: 20,
    };
    render();
    await screen.findByText("eu-west-1");
    fireEvent.click(screen.getByLabelText("Require on-prem runtimes"));
    expect(state.save).toHaveBeenCalledWith(
      { region_allowlist: ["eu-west-1"], banned_providers: [], require_on_prem: true },
      expect.anything(),
    );
  });

  it("hides every control from a reader who cannot edit", async () => {
    state.save.mockClear();
    state.policy = {
      region_allowlist: ["eu-west-1"],
      banned_providers: [],
      require_on_prem: false,
      max_list_length: 20,
    };
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    renderWithI18n(
      <QueryClientProvider client={qc}>
        <DataResidencySetting canEdit={false} />
      </QueryClientProvider>,
    );
    expect(await screen.findByText("eu-west-1")).toBeTruthy();
    expect(screen.queryByLabelText("Remove eu-west-1")).toBeNull();
    expect(screen.queryByLabelText("Allowed regions")).toBeNull();
  });
});
