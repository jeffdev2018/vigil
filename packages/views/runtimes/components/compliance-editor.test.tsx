// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AgentRuntime } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";

// Data residency (K46). The eligibility rules are exhaustively tested beside
// the helper in packages/core/residency/schemas.test.ts; this suite covers the
// states the reader must be able to tell apart on a runtime page — undeclared,
// declared-and-eligible, declared-but-rejected, no policy at all — plus the
// admin gate and the declare/clear wiring.

const TEST_RESOURCES = { en: { common: enCommon, runtimes: enRuntimes } };

const state = vi.hoisted(() => ({
  declare: vi.fn(),
  clear: vi.fn(),
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
  useDeclareRuntimeCompliance: () => ({ mutate: state.declare, isPending: false }),
  useClearRuntimeCompliance: () => ({ mutate: state.clear, isPending: false }),
}));

import { ComplianceEditor } from "./compliance-editor";

function makeRuntime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "rt-1",
    workspace_id: "ws-1",
    daemon_id: null,
    name: "Local Runtime",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "host.local",
    metadata: {},
    owner_id: "user-me",
    visibility: "private",
    last_seen_at: null,
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
    ...overrides,
  };
}

function renderEditor(runtime: AgentRuntime, canEdit = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ComplianceEditor runtime={runtime} canEdit={canEdit} />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.declare.mockClear();
  state.clear.mockClear();
  state.policy = { region_allowlist: [], banned_providers: [], require_on_prem: false, max_list_length: 20 };
});

describe("ComplianceEditor", () => {
  it("declares a region and its on-prem flag together", async () => {
    renderEditor(makeRuntime());
    const region = screen.getByLabelText("Declared region") as HTMLInputElement;
    fireEvent.change(region, { target: { value: "  eu-west-1 " } });
    fireEvent.click(screen.getByLabelText("On-prem machine"));
    fireEvent.click(screen.getByRole("button", { name: "Declare" }));
    expect(state.declare).toHaveBeenCalledWith(
      { runtimeId: "rt-1", declaration: { region: "eu-west-1", on_prem: true } },
      expect.anything(),
    );
  });

  it("refuses to declare an empty region rather than storing a blank one", () => {
    renderEditor(makeRuntime());
    const declare = screen.getByRole("button", { name: "Declare" }) as HTMLButtonElement;
    expect(declare.disabled).toBe(true);
    fireEvent.change(screen.getByLabelText("Declared region"), { target: { value: "   " } });
    expect((screen.getByRole("button", { name: "Declare" }) as HTMLButtonElement).disabled).toBe(true);
    expect(state.declare).not.toHaveBeenCalled();
  });

  it("offers Clear only once something is declared", () => {
    renderEditor(makeRuntime());
    expect(screen.queryByRole("button", { name: "Clear" })).toBeNull();

    renderEditor(makeRuntime({ compliance: { region: "eu-west-1", on_prem: true } }));
    fireEvent.click(screen.getAllByRole("button", { name: "Clear" })[0]!);
    expect(state.clear).toHaveBeenCalledWith("rt-1", expect.anything());
  });

  it("says the workspace has no policy rather than badging eligibility", async () => {
    renderEditor(makeRuntime());
    expect(await screen.findByText(/no data residency policy/)).toBeTruthy();
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("marks a compliant runtime eligible once a policy is live", async () => {
    state.policy = {
      region_allowlist: ["eu-west-1"],
      banned_providers: [],
      require_on_prem: true,
      max_list_length: 20,
    };
    renderEditor(makeRuntime({ compliance: { region: "eu-west-1", on_prem: true } }));
    expect(await screen.findByText("Eligible under this workspace's data residency policy.")).toBeTruthy();
  });

  it("explains the specific rule a rejected runtime fails", async () => {
    state.policy = {
      region_allowlist: [],
      banned_providers: [],
      require_on_prem: true,
      max_list_length: 20,
    };
    renderEditor(makeRuntime({ runtime_mode: "cloud", compliance: { region: "eu-west-1", on_prem: true } }));
    // A cloud machine can never be on-prem, whatever it declared — the copy
    // must name that rule rather than say "not compliant".
    expect(await screen.findByText(/requires on-prem runtimes/)).toBeTruthy();
  });

  it("shows a non-admin the declaration read-only", async () => {
    state.policy = {
      region_allowlist: ["eu-west-1"],
      banned_providers: [],
      require_on_prem: false,
      max_list_length: 20,
    };
    renderEditor(makeRuntime({ compliance: { region: "eu-west-1", on_prem: false } }), false);
    expect(screen.getByText("eu-west-1")).toBeTruthy();
    expect(screen.queryByLabelText("Declared region")).toBeNull();
    expect(screen.queryByRole("button", { name: "Declare" })).toBeNull();
    // Eligibility is still visible: a member must be able to see why their
    // agent's machine is or is not taking work.
    expect(await screen.findByText("Eligible under this workspace's data residency policy.")).toBeTruthy();
  });

  it("tells a non-admin when nothing has been declared at all", () => {
    renderEditor(makeRuntime(), false);
    expect(screen.getByText(/Nothing declared/)).toBeTruthy();
  });
});
