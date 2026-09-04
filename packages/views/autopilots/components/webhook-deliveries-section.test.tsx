import { describe, it, expect, vi } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { WebhookDelivery } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const deliveries = vi.hoisted(() => ({ rows: [] as WebhookDelivery[] }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-test" }));

vi.mock("@multica/core/autopilots", () => ({
  autopilotDeliveriesOptions: (wsId: string, autopilotId: string) => ({
    queryKey: ["deliveries", wsId, autopilotId],
    queryFn: async () => deliveries.rows,
  }),
  autopilotDeliveryOptions: (wsId: string, autopilotId: string, id: string) => ({
    queryKey: ["delivery", wsId, autopilotId, id],
    queryFn: async () => deliveries.rows.find((d) => d.id === id) ?? null,
  }),
  useReplayAutopilotDelivery: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { WebhookDeliveriesSection, reasonExplanation } from "./webhook-deliveries-section";

function delivery(overrides: Partial<WebhookDelivery> = {}): WebhookDelivery {
  return {
    id: "del-1",
    workspace_id: "ws-test",
    autopilot_id: "ap-1",
    trigger_id: "trg-1",
    provider: "github",
    event: "issues",
    dedupe_key: null,
    dedupe_source: null,
    signature_status: "not_required",
    status: "ignored",
    attempt_count: 1,
    dispatch_attempts: 0,
    available_at: "2026-01-01T00:00:00Z",
    content_type: "application/json",
    response_status: 200,
    autopilot_run_id: null,
    replayed_from_delivery_id: null,
    error: null,
    reason_code: null,
    replay_idempotency_key: null,
    received_at: "2026-01-01T00:00:00Z",
    last_attempt_at: "2026-01-01T00:00:00Z",
    created_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function renderSection(rows: WebhookDelivery[]) {
  deliveries.rows = rows;
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <WebhookDeliveriesSection autopilotId="ap-1" hasWebhookTrigger />
    </QueryClientProvider>,
  );
}

describe("reasonExplanation", () => {
  it("strips the code the server prefixes its explanation with", () => {
    expect(
      reasonExplanation("criteria_not_matched", "criteria_not_matched: no production impact"),
    ).toBe("no production impact");
  });

  it("keeps an explanation that does not repeat the code", () => {
    expect(reasonExplanation("quota_exceeded", "monthly run limit of 100 reached")).toBe(
      "monthly run limit of 100 reached",
    );
  });

  it("has nothing to say when the error is only the code again", () => {
    expect(reasonExplanation("event_filtered", "event_filtered")).toBeNull();
    expect(reasonExplanation("event_filtered", null)).toBeNull();
    expect(reasonExplanation(null, null)).toBeNull();
  });
});

describe("WebhookDeliveriesSection reason codes", () => {
  it("says why an ignored delivery produced nothing", async () => {
    renderSection([
      delivery({
        reason_code: "criteria_not_matched",
        error: "criteria_not_matched: the issue is not a production incident",
      }),
    ]);

    expect(await screen.findByText("Ignored")).toBeInTheDocument();
    expect(screen.getByText("Criteria not matched")).toBeInTheDocument();
  });

  it("renders an unknown code verbatim rather than dropping it", async () => {
    renderSection([delivery({ reason_code: "some_future_code" })]);

    expect(await screen.findByText("some_future_code")).toBeInTheDocument();
  });

  it("shows nothing extra when the server sent no reason", async () => {
    renderSection([delivery({ status: "dispatched", reason_code: null })]);

    expect(await screen.findByText("Dispatched")).toBeInTheDocument();
    expect(screen.queryByText("Criteria not matched")).not.toBeInTheDocument();
  });
});
