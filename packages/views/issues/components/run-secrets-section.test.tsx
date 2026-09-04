// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { RunSecret } from "@multica/core/issues/run-secrets";
import { renderWithI18n } from "../../test/i18n";

// Parsing and grouping: packages/core/issues/run-secrets.test.ts.

const state = vi.hoisted(() => ({ secrets: [] as RunSecret[] }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issues/run-secrets", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/run-secrets")>()),
  issueRunSecretsOptions: () => ({ queryKey: ["rs"], queryFn: async () => state.secrets }),
}));

import { RunSecretsSection } from "./run-secrets-section";

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <RunSecretsSection issueId="i1" />
    </QueryClientProvider>,
  );
}

const secret = (over: Partial<RunSecret>): RunSecret => ({
  id: "s", task_id: "0123456789abcdef", key: "API_KEY", status: "active", expires_at: "2026-09-04T10:00:00Z", revoked_at: null, revoke_reason: null, created_at: "2026-09-04T09:30:00Z", ...over,
});

beforeEach(() => {
  state.secrets = [];
});

describe("RunSecretsSection", () => {
  it("renders nothing without a secret", async () => {
    const { container } = render();
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toBe("");
  });

  it("lists each run's keys with their status and never a value", async () => {
    state.secrets = [secret({ id: "a" }), secret({ id: "b", key: "DB_URL", status: "revoked", revoked_at: "2026-09-04T09:45:00Z", revoke_reason: "run_finished" }), secret({ id: "c", task_id: "fedcba9876543210", key: "API_KEY", status: "expired" })];
    render();
    expect(await screen.findAllByText("API_KEY", { selector: "li span" })).toHaveLength(2);
    expect(screen.getAllByTestId("run-secrets-run")).toHaveLength(2);
    expect(screen.getByText("revoked")).toBeTruthy();
    expect(screen.getByText("expired")).toBeTruthy();
    expect(screen.getByText("Run 01234567")).toBeTruthy();
  });
});
