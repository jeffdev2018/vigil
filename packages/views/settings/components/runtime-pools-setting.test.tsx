// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { RuntimePool } from "@multica/core/runtimes/pools";
import type { AgentRuntime } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Opens a Select's popup by its trigger accessible name (scoped to `within`
// when given) and clicks the option whose accessible name matches.
async function pickOption(
  user: ReturnType<typeof userEvent.setup>,
  scope: { getByRole: typeof screen.getByRole },
  triggerName: string,
  optionName: string | RegExp,
) {
  await user.click(scope.getByRole("combobox", { name: triggerName }));
  await user.click(await screen.findByRole("option", { name: optionName }));
}

// Client parsing and list helpers: packages/core/runtimes/pools.test.ts.

const state = vi.hoisted(() => ({
  pools: [] as RuntimePool[],
  runtimes: [] as AgentRuntime[],
  save: vi.fn(),
  remove: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/runtimes", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/runtimes")>()),
  runtimeListOptions: () => ({ queryKey: ["rt"], queryFn: async () => state.runtimes }),
}));
vi.mock("@multica/core/runtimes/pools", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/runtimes/pools")>()),
  runtimePoolsOptions: () => ({ queryKey: ["pools"], queryFn: async () => state.pools }),
  useSaveRuntimePool: () => ({ mutate: state.save, isPending: false }),
  useDeleteRuntimePool: () => ({ mutate: state.remove, isPending: false }),
}));

import { RuntimePoolsSetting } from "./runtime-pools-setting";

const rt = (id: string, name: string, status = "online"): AgentRuntime => ({ id, name, status, provider: "claude" } as unknown as AgentRuntime);

function render(canEdit = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <RuntimePoolsSetting canEdit={canEdit} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.runtimes = [rt("a", "Codex (host)"), rt("b", "Claude (host)", "offline"), rt("c", "Local (ollama)")];
  state.pools = [{ id: "p1", name: "main", runtime_ids: ["a", "b"], degraded_runtime_id: null, agent_count: 2, created_at: "" }];
  state.save.mockReset();
  state.remove.mockReset();
});

describe("RuntimePoolsSetting", () => {
  it("lists a pool in preference order, reorders and saves with a degraded runtime", async () => {
    render();
    expect(await screen.findByText("Codex (host)")).toBeTruthy();
    expect(screen.getByText("2 agents")).toBeTruthy();
    fireEvent.click(screen.getByLabelText("Move Claude (host) up"));
    const user = userEvent.setup();
    await pickOption(user, screen, "Degraded runtime for main", "Local (ollama)");
    fireEvent.click(screen.getByRole("button", { name: "Save pool" }));
    expect(state.save).toHaveBeenCalledWith({ id: "p1", input: { name: "main", runtime_ids: ["b", "a"], degraded_runtime_id: "c" } }, expect.anything());
  });

  it("creates a pool from the picker and deletes one", async () => {
    render();
    fireEvent.click(await screen.findByRole("button", { name: "New pool" }));
    const cards = screen.getAllByTestId("runtime-pool-card");
    const fresh = cards[cards.length - 1] as HTMLElement;
    fireEvent.change(fresh.querySelector("input") as HTMLInputElement, { target: { value: "backup" } });
    const user = userEvent.setup();
    await pickOption(user, within(fresh), "Add a runtime…", "Local (ollama)");
    fireEvent.click(screen.getByRole("button", { name: "Save pool" }));
    expect(state.save).toHaveBeenCalledWith({ input: { name: "backup", runtime_ids: ["c"], degraded_runtime_id: null } }, expect.anything());
    fireEvent.click(screen.getByLabelText("Delete pool main"));
    expect(state.remove).toHaveBeenCalledWith("p1", expect.anything());
  });

  it("is read-only for viewers", async () => {
    render(false);
    expect(await screen.findByText("Codex (host)")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "New pool" })).toBeNull();
    expect(screen.queryByLabelText("Move Claude (host) up")).toBeNull();
  });
});
