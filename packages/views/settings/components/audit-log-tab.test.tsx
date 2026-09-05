// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AuditLogEntry } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Client parsing and keys: packages/core/workspace/audit.test.ts.

const state = vi.hoisted(() => ({
  entries: [] as AuditLogEntry[],
  filters: [] as unknown[],
  exportText: "id,action",
  chain: { ok: true, total: 3, head_hash: "abcdef0123456789", broken_seq: null, broken_id: null } as { ok: boolean; total: number; head_hash: string; broken_seq: number | null; broken_id: string | null },
  exportCalls: [] as unknown[],
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/api", () => ({
  api: {
    verifyAuditLog: vi.fn(async () => state.chain),
    exportAuditLog: vi.fn(async (format: string, filter: unknown) => {
      state.exportCalls.push([format, filter]);
      return state.exportText;
    }),
  },
}));
vi.mock("@multica/core/workspace/audit", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/workspace/audit")>()),
  auditLogInfiniteOptions: (wsId: string, filter: unknown) => {
    state.filters.push(filter);
    return {
      queryKey: ["audit-log", wsId, JSON.stringify(filter)],
      queryFn: async () => ({ entries: state.entries, next_cursor: "" }),
      initialPageParam: "",
      getNextPageParam: () => undefined,
    };
  },
}));

import { AuditLogTab } from "./audit-log-tab";

const entry = (over: Partial<AuditLogEntry> = {}): AuditLogEntry => ({
  id: "e1", workspace_id: "ws-1", occurred_at: "2026-09-04T00:00:00Z", actor_type: "member", actor_id: "u1",
  action: "issue.status_changed", entity_type: "issue", entity_id: "12345678-aaaa", model: null, cost_usd_ticks: null,
  approver_type: null, approver_id: null, details: { from: "todo", to: "done" }, ...over,
});

function renderTab() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <AuditLogTab />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.entries = [];
  state.filters = [];
  state.exportCalls = [];
  vi.stubGlobal("URL", { ...URL, createObjectURL: () => "blob:x", revokeObjectURL: () => undefined });
});

describe("AuditLogTab", () => {
  it("verifies the hash chain on demand and names a broken link", async () => {
    renderTab();
    await screen.findByTestId("audit-empty");
    fireEvent.click(screen.getByRole("button", { name: "Verify chain" }));
    const ok = await screen.findByTestId("audit-chain");
    expect(ok.getAttribute("data-ok")).toBe("true");
    expect(ok.textContent).toContain("abcdef012345");
    state.chain = { ok: false, total: 3, head_hash: "x", broken_seq: 2, broken_id: "e2" };
    fireEvent.click(screen.getByRole("button", { name: "Verify chain" }));
    await new Promise((r) => setTimeout(r, 0));
    expect(screen.getByTestId("audit-chain").getAttribute("data-ok")).toBe("false");
    expect(screen.getByTestId("audit-chain").textContent).toContain("#2");
  });

  it("says the log is empty and exports with the same filter as the view", async () => {
    renderTab();
    expect(await screen.findByTestId("audit-empty")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Actor"), { target: { value: "agent" } });
    fireEvent.change(screen.getByLabelText("Action"), { target: { value: "decision.answered" } });
    expect(state.filters.at(-1)).toEqual({ actor_type: "agent", action: "decision.answered" });
    fireEvent.click(screen.getByRole("button", { name: "Export CSV" }));
    await new Promise((r) => setTimeout(r, 0));
    expect(state.exportCalls[0]).toEqual(["csv", { actor_type: "agent", action: "decision.answered" }]);
  });

  it("lists entries with actor, action, entity and details", async () => {
    state.entries = [entry(), entry({ id: "e2", actor_type: "agent", action: "decision.asked", approver_id: null })];
    renderTab();
    const rows = await screen.findAllByTestId("audit-row");
    expect(rows).toHaveLength(2);
    expect(rows[0]?.textContent).toContain("issue.status_changed");
    expect(rows[0]?.textContent).toContain('{"from":"todo","to":"done"}');
    expect(rows[1]?.textContent).toContain("agent");
  });
});
