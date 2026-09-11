// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { RunLimitPolicy } from "@multica/core/budgets/run-limits";
import { renderWithI18n } from "../../test/i18n";

// Opens a Select's popup by its trigger accessible name and clicks the
// option whose accessible name matches.
async function pickOption(
  user: ReturnType<typeof userEvent.setup>,
  triggerName: string,
  optionName: string | RegExp,
) {
  await user.click(screen.getByRole("combobox", { name: triggerName }));
  await user.click(await screen.findByRole("option", { name: optionName }));
}

// Parsing and formatting: packages/core/budgets/run-limits.test.ts.

const state = vi.hoisted(() => ({
  policies: [] as RunLimitPolicy[],
  save: vi.fn(),
  remove: vi.fn(),
  settings: {} as Record<string, unknown>,
  updateWorkspace: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: async () => [{ id: "a1", name: "Builder" }] }),
  workspaceKeys: { list: () => ["workspaces"] },
}));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", slug: "acme", settings: state.settings }),
}));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: { updateWorkspace: (id: string, body: unknown) => state.updateWorkspace(id, body) },
}));
vi.mock("@multica/core/projects", () => ({ projectListOptions: () => ({ queryKey: ["projects"], queryFn: async () => [{ id: "p1", title: "Billing" }] }) }));
vi.mock("@multica/core/budgets/run-limits", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/budgets/run-limits")>()),
  runLimitPoliciesOptions: () => ({ queryKey: ["rl"], queryFn: async () => state.policies }),
  useSaveRunLimitPolicy: () => ({ mutate: state.save, isPending: false }),
  useDeleteRunLimitPolicy: () => ({ mutate: state.remove, isPending: false }),
}));

import { RunLimitsSection } from "./run-limits-section";

function render(canManage = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <RunLimitsSection canManage={canManage} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  cleanup();
  state.policies = [{ id: "r1", scope_type: "agent", scope_id: "a1", max_cost_usd_ticks: 20000000000, max_duration_seconds: 1800, max_turns: null, max_tool_calls: null, warn_bps: 8000, action: "enforce", created_at: "" }];
  state.save.mockReset();
  state.remove.mockReset();
  state.settings = { doctrine: { require_review: true } };
  state.updateWorkspace.mockReset().mockResolvedValue({ id: "ws-1", settings: {} });
});

describe("RunLimitsSection", () => {
  it("lists caps per scope and creates a new policy", async () => {
    render();
    expect(await screen.findByText("Agent · Builder")).toBeTruthy();
    expect(screen.getByText("Cost ≤ $2.00")).toBeTruthy();
    expect(screen.getByText("Duration ≤ 30m00s")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "New run limit" }));
    fireEvent.change(screen.getByLabelText("Turns"), { target: { value: "25" } });
    const user = userEvent.setup();
    await pickOption(user, "At the limit", "Observe only");
    fireEvent.click(screen.getByRole("button", { name: "Save run limit" }));
    expect(state.save).toHaveBeenCalledWith({ input: { scope_type: "workspace", scope_id: null, max_cost_usd_ticks: null, max_duration_seconds: null, max_turns: 25, max_tool_calls: null, warn_bps: 8000, action: "observe" } }, expect.anything());
  });

  it("deletes a policy", async () => {
    render();
    fireEvent.click(await screen.findByLabelText("Delete run limit for Agent · Builder"));
    expect(state.remove).toHaveBeenCalledWith("r1", expect.anything());
  });

  it("shows the server's default follow-up budget when nothing is stored", async () => {
    render();
    await screen.findByTestId("followup-budget");
    expect((screen.getByLabelText("Per agent, per day") as HTMLInputElement).value).toBe("20");
    expect((screen.getByLabelText("Per workspace, per day") as HTMLInputElement).value).toBe("200");
  });

  it("saves the whole settings blob with the follow-up key merged in", async () => {
    render();
    await screen.findByTestId("followup-budget");
    fireEvent.change(screen.getByLabelText("Per agent, per day"), { target: { value: "5" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(state.updateWorkspace).toHaveBeenCalled());
    // The endpoint replaces the blob, so every other setting must ride along.
    expect(state.updateWorkspace).toHaveBeenCalledWith("ws-1", {
      settings: {
        doctrine: { require_review: true },
        followups: { max_per_agent_per_day: 5, max_per_workspace_per_day: 200 },
      },
    });
  });

  it("refuses an out-of-range cap inline instead of sending it", async () => {
    render();
    await screen.findByTestId("followup-budget");
    fireEvent.change(screen.getByLabelText("Per agent, per day"), { target: { value: "1500" } });
    expect((await screen.findByRole("alert")).textContent).toContain("Between 1 and 1000");
    expect(screen.getByRole("button", { name: "Save" }).hasAttribute("disabled")).toBe(true);
    expect(state.updateWorkspace).not.toHaveBeenCalled();
  });

  it("stays read-only for members", async () => {
    render(false);
    expect(await screen.findByText("Agent · Builder")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "New run limit" })).toBeNull();
    expect(screen.queryByLabelText("Delete run limit for Agent · Builder")).toBeNull();
    expect((screen.getByLabelText("Per agent, per day") as HTMLInputElement).disabled).toBe(true);
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });
});
