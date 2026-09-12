// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";

// The shape derivation, the label key and the query hash are canonical in
// packages/core/insights/shape.test.ts. This suite covers the wiring: the
// question survives a failure, a timeout reads differently from a failure, a
// pinned card refreshes through /run alone, and every card shows its question.

const mockAskInsight = vi.hoisted(() => vi.fn());
const mockRunInsight = vi.hoisted(() => vi.fn());
const mockListInsightWidgets = vi.hoisted(() => vi.fn());
const mockCreateInsightWidget = vi.hoisted(() => vi.fn());
const mockDeleteInsightWidget = vi.hoisted(() => vi.fn());
const mockUpdateInsightWidget = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>("@multica/core/api");
  return {
    ...actual,
    api: {
      askInsight: (...args: unknown[]) => mockAskInsight(...args),
      runInsight: (...args: unknown[]) => mockRunInsight(...args),
      listInsightWidgets: (...args: unknown[]) => mockListInsightWidgets(...args),
      createInsightWidget: (...args: unknown[]) => mockCreateInsightWidget(...args),
      deleteInsightWidget: (...args: unknown[]) => mockDeleteInsightWidget(...args),
      updateInsightWidget: (...args: unknown[]) => mockUpdateInsightWidget(...args),
    },
  };
});
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}));

import { ApiError } from "@multica/core/api";
import { toast } from "sonner";
import { InsightsTab } from "./insights-tab";

const countQuery = { entity: "issue", metric: "count", group_by: [], filters: [] };

function renderTab() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <InsightsTab wsId="ws-1" />
    </QueryClientProvider>,
  );
}

describe("InsightsTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListInsightWidgets.mockResolvedValue([]);
    mockRunInsight.mockResolvedValue({ rows: [], shape: "number", warnings: [], duration_ms: 1 });
  });
  afterEach(() => vi.mocked(toast.error).mockClear());
  afterEach(() => cleanup());

  it("shows the answer and the question that produced it", async () => {
    mockAskInsight.mockResolvedValue({
      query: countQuery,
      rows: [{ value: 7 }],
      shape: "number",
      warnings: [],
      duration_ms: 5,
    });
    renderTab();

    await userEvent.type(
      screen.getByLabelText("Ask a question about this workspace"),
      "how many blocked issues",
    );
    await userEvent.click(screen.getByRole("button", { name: "Ask" }));

    expect(await screen.findByText("7")).toBeInTheDocument();
    expect(screen.getByText("how many blocked issues")).toBeInTheDocument();
  });

  it("keeps the question in the box when the translation fails", async () => {
    // Losing the only thing the user wrote is the fastest way to make someone
    // stop asking, so this is a requirement, not a nicety.
    mockAskInsight.mockRejectedValue(new ApiError("nope", 422, "Unprocessable Entity"));
    renderTab();

    const input = screen.getByLabelText("Ask a question about this workspace");
    await userEvent.type(input, "something the DSL cannot express");
    await userEvent.click(screen.getByRole("button", { name: "Ask" }));

    const error = await screen.findByTestId("insight-error");
    expect(error).toHaveAttribute("data-kind", "failed");
    expect(input).toHaveValue("something the DSL cannot express");
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });

  it("reads a timeout differently from a failure, and offers the retry", async () => {
    mockAskInsight.mockRejectedValue(new ApiError("too slow", 503, "Service Unavailable"));
    renderTab();

    await userEvent.type(screen.getByLabelText("Ask a question about this workspace"), "q");
    await userEvent.click(screen.getByRole("button", { name: "Ask" }));

    const error = await screen.findByTestId("insight-error");
    expect(error).toHaveAttribute("data-kind", "timeout");
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
  });

  it("says no row matched rather than rendering an empty chart", async () => {
    mockAskInsight.mockResolvedValue({
      query: { ...countQuery, group_by: ["status"] },
      rows: [],
      shape: "donut",
      warnings: [],
      duration_ms: 2,
    });
    renderTab();

    await userEvent.type(screen.getByLabelText("Ask a question about this workspace"), "q");
    await userEvent.click(screen.getByRole("button", { name: "Ask" }));

    expect(await screen.findByText("No row matches this question.")).toBeInTheDocument();
  });

  it("refreshes a pinned card through /run alone, and shows its question", async () => {
    mockListInsightWidgets.mockResolvedValue([
      {
        id: "w1",
        workspace_id: "ws-1",
        owner_id: "u1",
        name: "Blocked work",
        question: "how many issues are blocked",
        definition_version: 1,
        query: countQuery,
        display: {},
        visibility: "private",
        position: 0,
        revision: 1,
        created_at: "",
        updated_at: "",
      },
    ]);
    mockRunInsight.mockResolvedValue({
      rows: [{ value: 4 }],
      shape: "number",
      warnings: [],
      duration_ms: 3,
    });
    renderTab();

    expect(await screen.findByText("Blocked work")).toBeInTheDocument();
    expect(screen.getByText("how many issues are blocked")).toBeInTheDocument();
    expect(await screen.findByText("4")).toBeInTheDocument();
    // The whole point of a pinned document: no model call on refresh.
    expect(mockAskInsight).not.toHaveBeenCalled();
    expect(mockRunInsight).toHaveBeenCalledWith(countQuery, expect.anything());
  });

  it("renders the caveats the server attached to the number", async () => {
    mockAskInsight.mockResolvedValue({
      query: countQuery,
      rows: [{ value: 1 }],
      shape: "number",
      warnings: ["cycle time is creation to completion"],
      duration_ms: 1,
    });
    renderTab();

    await userEvent.type(screen.getByLabelText("Ask a question about this workspace"), "q");
    await userEvent.click(screen.getByRole("button", { name: "Ask" }));

    expect(
      await screen.findByText("cycle time is creation to completion"),
    ).toBeInTheDocument();
  });

  it("pins the document the server returned, not the text the user typed", async () => {
    mockAskInsight.mockResolvedValue({
      query: countQuery,
      rows: [{ value: 2 }],
      shape: "number",
      warnings: [],
      duration_ms: 1,
    });
    mockCreateInsightWidget.mockResolvedValue({ id: "w9" });
    renderTab();

    await userEvent.type(
      screen.getByLabelText("Ask a question about this workspace"),
      "how many issues",
    );
    await userEvent.click(screen.getByRole("button", { name: "Ask" }));
    await screen.findByText("2");
    await userEvent.click(screen.getByRole("button", { name: "Pin" }));

    await waitFor(() => {
      expect(mockCreateInsightWidget).toHaveBeenCalledWith({
        name: "how many issues",
        question: "how many issues",
        query: countQuery,
      });
    });
  });

  it("moves a card with buttons, carrying each row's own revision", async () => {
    // Two PATCHes, each with the revision of the row it edits: that is what
    // makes a concurrent edit lose on expected_revision instead of silently
    // winning.
    const widget = (id: string, position: number, revision: number) => ({
      id,
      workspace_id: "ws-1",
      owner_id: "u1",
      name: id,
      question: "q",
      definition_version: 1,
      query: countQuery,
      display: {},
      visibility: "private",
      position,
      revision,
      created_at: "",
      updated_at: "",
    });
    mockListInsightWidgets.mockResolvedValue([widget("first", 0, 3), widget("second", 1, 5)]);
    mockUpdateInsightWidget.mockResolvedValue(widget("first", 1, 4));
    renderTab();

    await screen.findByText("first");
    await userEvent.click(screen.getAllByRole("button", { name: "Move down" })[0]!);

    await waitFor(() => expect(mockUpdateInsightWidget).toHaveBeenCalledTimes(2));
    expect(mockUpdateInsightWidget).toHaveBeenCalledWith("first", {
      position: 1,
      expected_revision: 3,
    });
    expect(mockUpdateInsightWidget).toHaveBeenCalledWith("second", {
      position: 0,
      expected_revision: 5,
    });
  });

  it("falls back to a table past a dozen categories", async () => {
    const rows = Array.from({ length: 15 }, (_, i) => ({ status: `s${i}`, value: i + 1 }));
    mockAskInsight.mockResolvedValue({
      query: { ...countQuery, group_by: ["status"] },
      rows,
      shape: "donut",
      warnings: [],
      duration_ms: 1,
    });
    renderTab();

    await userEvent.type(screen.getByLabelText("Ask a question about this workspace"), "q");
    await userEvent.click(screen.getByRole("button", { name: "Ask" }));

    expect(await screen.findByText("15 categories — shown as a table.")).toBeInTheDocument();
  });

  it("still renders when the response carries a shape this build never heard of", async () => {
    mockAskInsight.mockResolvedValue({
      query: { ...countQuery, group_by: ["status"] },
      rows: [{ status: "blocked", value: 3 }],
      shape: "sankey",
      warnings: [],
      duration_ms: 1,
    });
    renderTab();

    await userEvent.type(screen.getByLabelText("Ask a question about this workspace"), "q");
    await userEvent.click(screen.getByRole("button", { name: "Ask" }));

    // Falls back to the locally derived donut: the legend row is the proof.
    expect(await screen.findByText("blocked")).toBeInTheDocument();
  });

  // Regression: usePinInsight/useUpdateInsightWidget/useDeleteInsightWidget
  // had no onError anywhere — a failed pin/reorder/remove silently
  // resynced on the next query settle with zero feedback.
  it("shows a toast when pinning the answer fails", async () => {
    mockAskInsight.mockResolvedValue({
      query: countQuery,
      rows: [{ value: 2 }],
      shape: "number",
      warnings: [],
      duration_ms: 1,
    });
    mockCreateInsightWidget.mockRejectedValue(new Error("server unavailable"));
    renderTab();

    await userEvent.type(screen.getByLabelText("Ask a question about this workspace"), "q");
    await userEvent.click(screen.getByRole("button", { name: "Ask" }));
    await screen.findByText("2");
    await userEvent.click(screen.getByRole("button", { name: "Pin" }));

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("server unavailable"));
  });

  it("shows a toast when reordering a card fails", async () => {
    const widget = (id: string, position: number, revision: number) => ({
      id,
      workspace_id: "ws-1",
      owner_id: "u1",
      name: id,
      question: "q",
      definition_version: 1,
      query: countQuery,
      display: {},
      visibility: "private",
      position,
      revision,
      created_at: "",
      updated_at: "",
    });
    mockListInsightWidgets.mockResolvedValue([widget("first", 0, 3), widget("second", 1, 5)]);
    mockUpdateInsightWidget.mockRejectedValue(new Error("conflict"));
    renderTab();

    await screen.findByText("first");
    await userEvent.click(screen.getAllByRole("button", { name: "Move down" })[0]!);

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("conflict"));
  });

  it("shows a toast when removing a card fails", async () => {
    mockListInsightWidgets.mockResolvedValue([
      {
        id: "w1",
        workspace_id: "ws-1",
        owner_id: "u1",
        name: "Blocked work",
        question: "q",
        definition_version: 1,
        query: countQuery,
        display: {},
        visibility: "private",
        position: 0,
        revision: 1,
        created_at: "",
        updated_at: "",
      },
    ]);
    mockDeleteInsightWidget.mockRejectedValue(new Error("in use"));
    renderTab();

    await screen.findByText("Blocked work");
    await userEvent.click(screen.getByRole("button", { name: "Remove" }));

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("in use"));
  });
});
