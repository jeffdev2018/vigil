// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { TriageItemsResponse, TriageStats } from "@multica/core/types";
import { toast } from "sonner";
import { renderWithI18n } from "../../test/i18n";
import { TriagePage } from "./triage-page";

// The heavy Markdown renderer is out of scope here — the page only needs to
// show that the captured description is threaded into the detail pane.
vi.mock("../../rich-content", () => ({
  RichContent: ({ content }: { content: string }) => (
    <div data-testid="rich-content">{content}</div>
  ),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

const data = vi.hoisted(() => ({
  stats: {
    pending: 1,
    shadow_pending: 0,
    dropped_24h: 0,
    oldest_pending_age_seconds: 3600,
    sources: [
      {
        id: "src-1",
        kind: "autopilot_webhook",
        ref_id: "ap-1",
        name: "Sentry",
        mode: "gate",
        items_24h: 3,
        dropped_24h: 0,
      },
    ],
  } as TriageStats,
  items: {
    items: [
      {
        id: "item-1",
        source_id: "src-1",
        source_name: "Sentry",
        source_kind: "autopilot_webhook",
        origin_type: "autopilot",
        title: "Payment gateway timeout",
        body_markdown: "Gateway timed out",
        payload: { size: 10, body: { alert: "payment-gateway" } },
        state: "pending",
        collapse_count: 2,
        first_seen_at: "2026-01-01T00:00:00Z",
        revision: 1,
      },
    ],
    next_cursor: undefined,
  } as TriageItemsResponse,
  itemsPage2: {
    items: [
      {
        id: "item-2",
        source_id: "src-1",
        source_name: "Sentry",
        source_kind: "autopilot_webhook",
        origin_type: "autopilot",
        title: "Second page delivery",
        body_markdown: "",
        payload: {},
        state: "pending",
        collapse_count: 1,
        first_seen_at: "2025-12-31T00:00:00Z",
        revision: 1,
      },
    ],
    next_cursor: undefined,
  } as TriageItemsResponse,
  dismissed: {
    items: [
      {
        id: "item-9",
        source_id: "src-1",
        source_name: "Sentry",
        source_kind: "autopilot_webhook",
        origin_type: "autopilot",
        title: "Auto-dismissed noise",
        body_markdown: "",
        payload: {},
        state: "dismissed",
        collapse_count: 1,
        resolution_reason: "auto: 92% confidence from 10 similar deliveries",
        first_seen_at: "2026-01-01T00:00:00Z",
        revision: 2,
      },
    ],
    next_cursor: undefined,
  } as TriageItemsResponse,
}));

vi.mock("@multica/core/triage/queries", () => ({
  triageStatsOptions: () => ({
    queryKey: ["triage", "ws-1", "stats"],
    queryFn: async () => data.stats,
  }),
  triageItemsInfiniteOptions: (_wsId: string, state: string) => ({
    queryKey: ["triage", "ws-1", "items", state],
    queryFn: async ({ pageParam }: { pageParam: string }) => {
      if (state === "dismissed") return data.dismissed;
      if (state !== "pending") return { items: [] };
      return pageParam ? data.itemsPage2 : data.items;
    },
    initialPageParam: "",
    getNextPageParam: (last: { next_cursor?: string }) => last.next_cursor || undefined,
  }),
  triageSuggestionsOptions: () => ({
    queryKey: ["triage", "ws-1", "suggestions", ""],
    queryFn: async () => ({ suggestions: {}, auto: { enabled: false, threshold: 0.9, min_examples: 20 } }),
    enabled: false,
  }),
}));

const mutations = vi.hoisted(() => ({
  reopen: vi.fn(),
  accept: vi.fn().mockResolvedValue({ item_id: "item-1", state: "accepted" }),
  dismiss: vi.fn().mockResolvedValue({ item_id: "item-1", state: "dismissed" }),
  batch: vi.fn().mockResolvedValue({ items: [] }),
  updateMode: vi.fn(),
}));

vi.mock("@multica/core/triage/mutations", () => ({
  useReopenTriageItem: () => ({ mutate: mutations.reopen, isPending: false }),
  useAcceptTriageItem: () => ({ mutateAsync: mutations.accept, isPending: false }),
  useDismissTriageItem: () => ({ mutateAsync: mutations.dismiss, isPending: false }),
  useBatchAcceptTriageItems: () => ({ mutateAsync: mutations.batch, isPending: false }),
  useUpdateTriageSourceMode: () => ({ mutate: mutations.updateMode, isPending: false }),
}));

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <TriagePage />
    </QueryClientProvider>,
  );
}

describe("TriagePage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    data.items.next_cursor = undefined;
    data.items.items = [
      {
        id: "item-1",
        source_id: "src-1",
        source_name: "Sentry",
        source_kind: "autopilot_webhook",
        origin_type: "autopilot",
        title: "Payment gateway timeout",
        body_markdown: "Gateway timed out",
        payload: { size: 10, body: { alert: "payment-gateway" } },
        state: "pending",
        collapse_count: 2,
        first_seen_at: "2026-01-01T00:00:00Z",
        revision: 1,
      },
    ];
  });

  it("renders pending items and the source strip", async () => {
    renderPage();
    expect(await screen.findByText("Payment gateway timeout")).toBeTruthy();
    // Source strip shows the source name (also echoed on the row) and its
    // current mode label.
    expect(screen.getAllByText("Sentry").length).toBeGreaterThan(0);
    expect(screen.getByText("Gate")).toBeTruthy();
    // Collapse count surfaces as a ×N badge.
    expect(screen.getByText("×2")).toBeTruthy();
  });

  it("shows the empty state when the queue is clear", async () => {
    data.items.items = [];
    renderPage();
    expect(await screen.findByText("Queue is clear")).toBeTruthy();
  });

  it("selecting an item shows its description and captured payload", async () => {
    renderPage();
    const row = await screen.findByText("Payment gateway timeout");
    fireEvent.click(row);
    expect(await screen.findByTestId("rich-content")).toBeTruthy();
    expect(screen.getByText(/payment-gateway/)).toBeTruthy();
  });

  it("accepting the selected item calls the mutation and toasts", async () => {
    renderPage();
    const row = await screen.findByText("Payment gateway timeout");
    fireEvent.click(row);
    const acceptButton = await screen.findByRole("button", { name: /^accept$/i });
    fireEvent.click(acceptButton);
    await waitFor(() => expect(mutations.accept).toHaveBeenCalledWith("item-1"));
    expect(toast.success).toHaveBeenCalled();
  });

  // History tabs (pending / accepted / dismissed / merged). Without them the
  // list only ever asked for pending, so a dismissed item — and the Reopen
  // button that only renders for one — was unreachable from the UI.
  it("the dismissed tab lists resolved items, their reason and a Reopen button", async () => {
    renderPage();
    await screen.findByText("Payment gateway timeout");

    fireEvent.click(screen.getByRole("button", { name: "Dismissed" }));
    const row = await screen.findByText("Auto-dismissed noise");
    fireEvent.click(row);

    expect(
      (await screen.findByTestId("triage-resolution-reason")).textContent,
    ).toContain("auto: 92% confidence from 10 similar deliveries");
    // A resolved item cannot be accepted or dismissed again.
    expect(screen.queryByRole("button", { name: /^accept$/i })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: /reopen/i }));
    expect(mutations.reopen).toHaveBeenCalledWith("item-9", expect.anything());
  });

  // The server has always returned next_cursor; before this the page dropped
  // it and the queue silently stopped at the first page.
  it("walks next_cursor through a Load more button", async () => {
    data.items.next_cursor = "cursor-1";
    renderPage();
    await screen.findByText("Payment gateway timeout");
    expect(screen.queryByText("Second page delivery")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: /load more/i }));
    expect(await screen.findByText("Second page delivery")).toBeTruthy();
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: /load more/i })).toBeNull(),
    );
  });

  it("checking a row reveals the batch-accept bar", async () => {
    renderPage();
    await screen.findByText("Payment gateway timeout");
    const checkbox = screen.getByRole("checkbox");
    fireEvent.click(checkbox);
    expect(await screen.findByRole("button", { name: /clear selection/i })).toBeTruthy();
  });
});
