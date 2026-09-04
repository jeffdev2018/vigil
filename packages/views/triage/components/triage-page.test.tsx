// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { TriageItemsResponse, TriageStats } from "@multica/core/types";
import { toast } from "sonner";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
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
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}`, meetingDetail: (id: string) => `/acme/meetings/${id}` }),
}));

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

const push = vi.hoisted(() => vi.fn());

const mutations = vi.hoisted(() => ({
  reopen: vi.fn(),
  accept: vi.fn().mockResolvedValue({
    item_id: "item-1",
    state: "accepted",
    issue: { id: "issue-1", identifier: "ACM-42" },
  }),
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

function renderPage(search = "") {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const adapter: NavigationAdapter = {
    push,
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/triage",
    searchParams: new URLSearchParams(search),
    hash: "",
    getShareableUrl: (p) => p,
  };
  return renderWithI18n(
    <NavigationProvider value={adapter}>
      <QueryClientProvider client={client}>
        <TriagePage />
      </QueryClientProvider>
    </NavigationProvider>,
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

  it("accepting the selected item names the created issue and links to it", async () => {
    renderPage();
    const row = await screen.findByText("Payment gateway timeout");
    fireEvent.click(row);
    const acceptButton = await screen.findByRole("button", { name: /^accept$/i });
    fireEvent.click(acceptButton);
    await waitFor(() => expect(mutations.accept).toHaveBeenCalledWith("item-1"));

    // The accept response carries the issue; a bare "done" toast threw it away.
    const [message, options] = vi.mocked(toast.success).mock.calls.at(-1) as [
      string,
      { action: { label: string; onClick: () => void } },
    ];
    expect(message).toContain("ACM-42");
    options.action.onClick();
    expect(push).toHaveBeenCalledWith("/acme/issues/issue-1");
  });

  it("batch accept reports the per-item outcomes the server returned", async () => {
    mutations.batch.mockResolvedValueOnce({
      items: [
        { id: "a", outcome: "accepted" },
        { id: "b", outcome: "duplicate" },
        { id: "c", outcome: "error" },
      ],
    });
    renderPage();
    await screen.findByText("Payment gateway timeout");
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(await screen.findByRole("button", { name: /^accept 1$/i }));

    await waitFor(() =>
      expect(vi.mocked(toast.success).mock.calls.at(-1)?.[0]).toBe(
        "1 accepted, 1 duplicates, 1 failed",
      ),
    );
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

  // Deep link + provenance: a meeting action links at the exact item it
  // produced, and the item links back at the meeting it came from.
  it("preselects the ?item= entry and links back to its meeting", async () => {
    data.items.items[0] = {
      ...data.items.items[0]!,
      origin_type: "meeting",
      origin_id: "meet-1",
    };
    renderPage("item=item-1");
    expect(await screen.findByTestId("rich-content")).toBeTruthy();
    const link = screen.getByRole("link", { name: /from meeting/i });
    expect(link.getAttribute("href")).toBe("/acme/meetings/meet-1");
  });

  it("checking a row reveals the batch-accept bar", async () => {
    renderPage();
    await screen.findByText("Payment gateway timeout");
    const checkbox = screen.getByRole("checkbox");
    fireEvent.click(checkbox);
    expect(await screen.findByRole("button", { name: /clear selection/i })).toBeTruthy();
  });
});
