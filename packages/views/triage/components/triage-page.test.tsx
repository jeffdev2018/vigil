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
// The "Accept as…" project picker and the verdict attribution both subscribe
// to workspace lists; the queue page is not the place to exercise those.
vi.mock("@multica/core/projects/queries", () => ({
  projectListOptions: () => ({ queryKey: ["projects", "ws-1"], queryFn: async () => [] }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getAgentName: (id: string) => `Agent ${id}` }),
}));
// The issue picker has its own suite; here the seam under test is what the
// queue does with the issue that comes back out of it.
vi.mock("../../modals/issue-picker-modal", () => ({
  IssuePickerModal: ({ onSelect }: { onSelect: (issue: { id: string; identifier: string }) => void }) => (
    <button type="button" onClick={() => onSelect({ id: "issue-9", identifier: "ACM-9" })}>
      pick ACM-9
    </button>
  ),
}));
// "Create a rule from this item" reuses the settings form; the rule API has
// its own suite (settings/components/business-rules-setting.test.tsx).
vi.mock("@multica/core/workspace/business-rules", () => ({
  businessRulesOptions: (wsId: string) => ({
    queryKey: ["business-rules", wsId],
    queryFn: async () => ({ rules: [], attach_points: ["webhook_received"] }),
  }),
  useCreateBusinessRule: () => ({ isPending: false, mutate: vi.fn() }),
  useDryRunBusinessRule: () => ({ isPending: false, mutate: vi.fn() }),
  useSetBusinessRuleStatus: () => ({ isPending: false, mutate: vi.fn() }),
}));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}`, meetingDetail: (id: string) => `/acme/meetings/${id}`, settings: () => "/acme/settings" }),
}));

const data = vi.hoisted(() => ({
  stats: {
    pending: 1,
    shadow_pending: 0,
    dropped_24h: 0,
    snoozed: 2,
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
  snoozed: {
    items: [
      {
        id: "item-7",
        source_id: "src-1",
        source_name: "Sentry",
        source_kind: "autopilot_webhook",
        origin_type: "autopilot",
        title: "Parked until Monday",
        body_markdown: "",
        payload: {},
        state: "pending",
        collapse_count: 1,
        snoozed_until: "2099-01-01T00:00:00Z",
        first_seen_at: "2026-01-01T00:00:00Z",
        revision: 1,
      },
      {
        id: "item-8",
        source_id: "src-1",
        source_name: "Sentry",
        source_kind: "autopilot_webhook",
        origin_type: "autopilot",
        title: "Already due again",
        body_markdown: "",
        payload: {},
        state: "pending",
        collapse_count: 1,
        first_seen_at: "2026-01-01T00:00:00Z",
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
  triageItemsInfiniteOptions: (_wsId: string, state: string, includeSnoozed = false) => ({
    queryKey: ["triage", "ws-1", "items", state, includeSnoozed],
    queryFn: async ({ pageParam }: { pageParam: string }) => {
      listCalls.push({ state, includeSnoozed });
      if (state === "dismissed") return data.dismissed;
      if (state !== "pending") return { items: [] };
      if (includeSnoozed) return data.snoozed;
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
const listCalls = vi.hoisted(() => [] as { state: string; includeSnoozed: boolean }[]);

const mutations = vi.hoisted(() => ({
  reopen: vi.fn(),
  merge: vi.fn().mockResolvedValue({ item_id: "item-1", state: "merged" }),
  snooze: vi.fn().mockResolvedValue({ item_id: "item-1", state: "pending" }),
  batchDismiss: vi.fn().mockResolvedValue({ items: [] }),
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
  useMergeTriageItem: () => ({ mutateAsync: mutations.merge, isPending: false }),
  useSnoozeTriageItem: () => ({ mutateAsync: mutations.snooze, isPending: false }),
  useBatchDismissTriageItems: () => ({ mutateAsync: mutations.batchDismiss, isPending: false }),
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
    listCalls.length = 0;
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
    await waitFor(() =>
      expect(mutations.accept).toHaveBeenCalledWith({ itemId: "item-1", overrides: {} }),
    );

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
    expect(screen.queryByRole("button", { name: /merge into/i })).toBeNull();

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

  // Snooze: an item parked in the future is still `pending`, so the tab has to
  // ask for it explicitly and then keep only what is actually parked — the
  // widened listing also carries the due ones.
  it("the Snoozed tab asks for snoozed items and lists only the parked ones", async () => {
    renderPage();
    await screen.findByText("Payment gateway timeout");

    fireEvent.click(screen.getByRole("button", { name: /^snoozed/i }));
    expect(await screen.findByText("Parked until Monday")).toBeTruthy();
    expect(screen.queryByText("Already due again")).toBeNull();
    expect(listCalls).toContainEqual({ state: "pending", includeSnoozed: true });
  });

  it("shows an agent verdict on the row and names the agent in the detail", async () => {
    data.items.items[0] = {
      ...data.items.items[0]!,
      verdict: "dismiss",
      verdict_reason: "duplicate alert noise",
      verdict_agent_id: "agent-1",
      verdict_at: "2026-01-01T00:05:00Z",
    };
    renderPage();
    expect(await screen.findByTestId("triage-verdict-badge")).toBeTruthy();

    fireEvent.click(screen.getByText("Payment gateway timeout"));
    const note = await screen.findByTestId("triage-verdict-note");
    expect(note.textContent).toContain("duplicate alert noise");
    expect(note.textContent).toContain("Agent agent-1");
  });

  it("dismissing sends the optional reason typed in the popover", async () => {
    renderPage();
    fireEvent.click(await screen.findByText("Payment gateway timeout"));
    fireEvent.click(screen.getByRole("button", { name: /^dismiss$/i }));

    const reason = await screen.findByLabelText(/reason/i);
    fireEvent.change(reason, { target: { value: "alert storm" } });
    fireEvent.click(screen.getByRole("button", { name: /confirm dismiss/i }));

    await waitFor(() =>
      expect(mutations.dismiss).toHaveBeenCalledWith({
        itemId: "item-1",
        reason: "alert storm",
      }),
    );
  });

  it("snoozing an item sends a future timestamp", async () => {
    renderPage();
    fireEvent.click(await screen.findByText("Payment gateway timeout"));
    fireEvent.click(screen.getByRole("button", { name: /^snooze$/i }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /in an hour/i }));

    await waitFor(() => expect(mutations.snooze).toHaveBeenCalled());
    const arg = mutations.snooze.mock.calls.at(-1)?.[0] as { itemId: string; until: string };
    expect(arg.itemId).toBe("item-1");
    expect(new Date(arg.until).getTime()).toBeGreaterThan(Date.now());
  });

  it("batch dismiss reports what the server actually dismissed", async () => {
    mutations.batchDismiss.mockResolvedValueOnce({
      items: [
        { id: "a", outcome: "dismissed" },
        { id: "b", outcome: "not_pending" },
      ],
    });
    renderPage();
    await screen.findByText("Payment gateway timeout");
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(await screen.findByRole("button", { name: /^dismiss 1$/i }));
    fireEvent.click(await screen.findByRole("button", { name: /confirm dismiss/i }));

    await waitFor(() =>
      expect(vi.mocked(toast.success).mock.calls.at(-1)?.[0]).toBe("1 dismissed, 1 failed"),
    );
  });

  // "Accept as…": the queue can set the issue's assignee, project and priority
  // instead of inheriting whatever the origin autopilot would have given it.
  it("offers the Accept as controls on a pending item", async () => {
    renderPage();
    fireEvent.click(await screen.findByText("Payment gateway timeout"));
    expect(await screen.findByText("Accept as")).toBeTruthy();
    expect(screen.getByText("Assignee")).toBeTruthy();
    expect(screen.getByText("Priority")).toBeTruthy();
    expect(screen.getByText("Project")).toBeTruthy();
  });

  it("merging folds the item into the picked issue", async () => {
    renderPage();
    fireEvent.click(await screen.findByText("Payment gateway timeout"));
    fireEvent.click(screen.getByRole("button", { name: /merge into/i }));
    fireEvent.click(await screen.findByRole("button", { name: /pick acm-9/i }));

    await waitFor(() =>
      expect(mutations.merge).toHaveBeenCalledWith({ itemId: "item-1", issueId: "issue-9" }),
    );
    await waitFor(() =>
      expect(vi.mocked(toast.success).mock.calls.at(-1)?.[0]).toContain("ACM-9"),
    );
  });

  // Keyboard: the focused row is the cursor, so J/K (and Up/Down) only move
  // focus and the row's own Enter opens the detail. Chord parsing itself is
  // covered by packages/core/shortcuts/definitions.test.ts.
  it("J/K move through the rows and Enter opens the focused item", async () => {
    data.items.items = [
      data.items.items[0]!,
      { ...data.items.items[0]!, id: "item-2", title: "Second delivery" },
    ];
    renderPage();
    await screen.findByText("Payment gateway timeout");
    const rows = document.querySelectorAll<HTMLElement>("[data-triage-row]");
    expect(rows).toHaveLength(2);

    fireEvent.keyDown(document, { key: "j" });
    expect(document.activeElement).toBe(rows[0]);
    fireEvent.keyDown(document, { key: "j" });
    expect(document.activeElement).toBe(rows[1]);
    // Up/Down are list navigation, not a rebindable action: they only work
    // from a focused row.
    fireEvent.keyDown(rows[1]!, { key: "ArrowUp" });
    expect(document.activeElement).toBe(rows[0]);
    fireEvent.keyDown(document, { key: "k" });
    expect(document.activeElement).toBe(rows[0]);

    fireEvent.keyDown(rows[0]!, { key: "Enter" });
    expect(await screen.findByTestId("rich-content")).toBeTruthy();

    // Escape closes the detail again.
    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() => expect(screen.queryByTestId("rich-content")).toBeNull());
  });

  it("A accepts the open item", async () => {
    renderPage();
    fireEvent.click(await screen.findByText("Payment gateway timeout"));
    await screen.findByRole("button", { name: /^accept$/i });

    fireEvent.keyDown(document, { key: "a" });
    await waitFor(() =>
      expect(mutations.accept).toHaveBeenCalledWith({ itemId: "item-1", overrides: {} }),
    );
  });

  it("does not fire a binding while the keyboard belongs to a field", async () => {
    renderPage();
    fireEvent.click(await screen.findByText("Payment gateway timeout"));
    fireEvent.click(await screen.findByRole("button", { name: /^dismiss$/i }));

    const reason = await screen.findByLabelText(/reason/i);
    fireEvent.keyDown(reason, { key: "a" });
    fireEvent.keyDown(reason, { key: "x" });
    expect(mutations.accept).not.toHaveBeenCalled();
    expect(mutations.dismiss).not.toHaveBeenCalled();
  });

  // Rules (K62) are recognized in the queue, not in Settings: the item in front
  // of the human prefills the rule, and the header links at the full editor.
  it("drafts a rule prefilled from the open item, and links at the rule settings", async () => {
    renderPage();
    fireEvent.click(await screen.findByText("Payment gateway timeout"));
    expect(
      screen.getByRole("link", { name: /manage rules/i }).getAttribute("href"),
    ).toBe("/acme/settings?tab=workspace");

    fireEvent.click(await screen.findByRole("button", { name: /^create a rule$/i }));

    const rule = await screen.findByLabelText("Rule");
    expect((rule as HTMLTextAreaElement).value).toBe(
      'Deliveries from Sentry whose title contains "Payment"',
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
