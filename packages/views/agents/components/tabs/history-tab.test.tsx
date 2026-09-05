// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent, AgentVersion, AgentVersionDiff } from "@multica/core/types";
import { renderWithI18n } from "../../../test/i18n";

// Line diff and schema fallbacks: packages/core/agents/versions.test.ts.

const state = vi.hoisted(() => ({
  versions: [] as AgentVersion[],
  diff: null as AgentVersionDiff | null,
  rollback: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/agents/versions", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/agents/versions")>()),
  agentVersionsOptions: (wsId: string, agentId: string) => ({ queryKey: ["agent-versions", wsId, agentId], queryFn: async () => state.versions }),
  agentVersionDiffOptions: (wsId: string, agentId: string, v: string, against: string) => ({ queryKey: ["agent-versions", wsId, agentId, "diff", v, against], queryFn: async () => state.diff }),
  useRollbackAgentVersion: () => ({ mutate: state.rollback, isPending: false }),
}));

import { HistoryTab } from "./history-tab";

const version = (n: number, over: Partial<AgentVersion> = {}): AgentVersion => ({
  id: `v${n}`, agent_id: "a1", version_number: n, instructions: `Rule ${n}`, model: "m", skill_ids: [], tool_config: {},
  created_by_type: n === 1 ? "system" : "member", created_by_id: null, created_at: "2026-09-03T00:00:00Z", active: false, ...over,
});

function renderTab(canEdit = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <HistoryTab agent={{ id: "a1" } as Agent} canEdit={canEdit} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.versions = [];
  state.diff = null;
  state.rollback.mockReset();
});

describe("HistoryTab", () => {
  it("says there is no history with a single version", async () => {
    state.versions = [version(1, { active: true })];
    renderTab();
    expect((await screen.findByTestId("agent-history")).dataset.empty).toBe("true");
  });

  it("lists versions newest first, diffs a selected one against the active one, and rolls back after a confirmation", async () => {
    state.versions = [version(2, { active: true }), version(1)];
    state.diff = { from: version(1), to: version(2, { active: true }), changed_fields: ["instructions"] };
    renderTab();
    const rows = await screen.findAllByTestId("agent-version");
    expect(rows.map((r) => r.dataset.active)).toEqual(["true", "false"]);
    fireEvent.click(screen.getByText("v1"));
    const diff = await screen.findByTestId("agent-version-diff");
    expect(diff.textContent).toContain("- Rule 1");
    expect(diff.textContent).toContain("+ Rule 2");
    fireEvent.click(screen.getByRole("button", { name: "Roll back" }));
    expect(state.rollback).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Confirm rollback to v1" }));
    expect(state.rollback).toHaveBeenCalledWith("v1", expect.anything());
  });

  it("offers no rollback to a reader", async () => {
    state.versions = [version(2, { active: true }), version(1)];
    renderTab(false);
    await screen.findAllByTestId("agent-version");
    expect(screen.queryByRole("button", { name: "Roll back" })).toBeNull();
  });
});
