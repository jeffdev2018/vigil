import { describe, it, expect, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { WebhookDelivery } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const deliveries = vi.hoisted(() => ({ rows: [] as WebhookDelivery[], total: 0, pageSize: 20 }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-test" }));

vi.mock("@multica/core/autopilots", () => ({
  // Mirrors the real infiniteQueryOptions shape: an offset pager over a slim
  // page plus the server-side total.
  autopilotDeliveriesOptions: (wsId: string, autopilotId: string) => ({
    queryKey: ["deliveries", wsId, autopilotId],
    queryFn: async ({ pageParam }: { pageParam: number }) => ({
      deliveries: deliveries.rows.slice(pageParam, pageParam + deliveries.pageSize),
      total: deliveries.total,
    }),
    initialPageParam: 0,
    getNextPageParam: (
      lastPage: { total: number },
      allPages: { deliveries: WebhookDelivery[] }[],
    ) => {
      const loaded = allPages.reduce((n, p) => n + p.deliveries.length, 0);
      return loaded >= lastPage.total ? undefined : loaded;
    },
    select: (data: { pages: { deliveries: WebhookDelivery[]; total: number }[] }) => ({
      items: data.pages.flatMap((page) => page.deliveries),
      total: data.pages[data.pages.length - 1]?.total ?? 0,
    }),
  }),
  autopilotDeliveryOptions: (wsId: string, autopilotId: string, id: string) => ({
    queryKey: ["delivery", wsId, autopilotId, id],
    queryFn: async () => deliveries.rows.find((d) => d.id === id) ?? null,
  }),
  useReplayAutopilotDelivery: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useDryRunAutopilotWebhookTrigger: () => ({ mutateAsync: vi.fn(), isPending: false }),
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

function renderSection(rows: WebhookDelivery[], pageSize = 20) {
  deliveries.rows = rows;
  deliveries.total = rows.length;
  deliveries.pageSize = pageSize;
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

describe("WebhookDeliveriesSection paging", () => {
  it("reports the server's total, not the page it got, and pages to the rest", async () => {
    const user = userEvent.setup();
    const rows = Array.from({ length: 3 }, (_, i) =>
      delivery({ id: `del-${i}`, event: `event-${i}` }),
    );
    renderSection(rows, 2);

    expect(await screen.findByText("2 of 3")).toBeInTheDocument();
    expect(screen.queryByText("event-2")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Load more" }));

    expect(await screen.findByText("event-2")).toBeInTheDocument();
    expect(screen.getByText("3 of 3")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument();
  });

  it("offers no Load more when the first page is everything", async () => {
    renderSection([delivery()], 20);
    expect(await screen.findByText("1 of 1")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument();
  });
});
