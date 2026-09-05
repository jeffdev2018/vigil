// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { RunFailover } from "@multica/core/runtimes/pools";
import { renderWithI18n } from "../../test/i18n";

// Parsing: packages/core/runtimes/pools.test.ts.

const state = vi.hoisted(() => ({ runs: [] as RunFailover[] }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/runtimes", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/runtimes")>()),
  runtimeListOptions: () => ({ queryKey: ["rt"], queryFn: async () => [{ id: "a", name: "Codex (host)", provider: "codex" }, { id: "d", name: "Local (ollama)", provider: "ollama" }] }),
}));
vi.mock("@multica/core/runtimes/pools", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/runtimes/pools")>()),
  issueFailoverHistoryOptions: () => ({ queryKey: ["fo"], queryFn: async () => state.runs }),
}));

import { FailoverSection } from "./failover-section";

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <FailoverSection issueId="i1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.runs = [];
});

describe("FailoverSection", () => {
  it("renders nothing without a move", async () => {
    const { container } = render();
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toBe("");
  });

  it("shows moves with runtime names and an explicit banner while a run is degraded", async () => {
    state.runs = [
      { task_id: "0123456789", status: "running", degraded: true, moves: [{ from_runtime_id: "a", to_runtime_id: "d", reason: "runtime_offline", degraded: true, at: "2026-09-04T10:00:00Z" }] },
      { task_id: "abcdef0123", status: "failed", degraded: false, failure_reason: "runtime_pool_exhausted", moves: [{ from_runtime_id: "a", to_runtime_id: "zz", reason: "agent_error.provider_server_error", degraded: false, at: "" }] },
    ];
    render();
    expect(await screen.findByTestId("degraded-banner")).toBeTruthy();
    expect(await screen.findByText("Local (ollama)")).toBeTruthy();
    expect(screen.getAllByTestId("failover-run")).toHaveLength(2);
    expect(screen.getByText("Pool exhausted")).toBeTruthy();
    expect(screen.getByText(/Runtime offline/)).toBeTruthy();
  });
});
