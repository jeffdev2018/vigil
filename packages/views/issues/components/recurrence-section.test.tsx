// @vitest-environment jsdom
import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { IssueRecurrenceResponse } from "@multica/core/types";
import { ApiError } from "@multica/core/api";
import en from "../../locales/en/issues.json";
import { renderWithI18n } from "../../test/i18n";

// Client parsing, the preset↔cron matrix and the cache updaters are covered in
// packages/core/recurrence/recurrence.test.ts. This suite keeps the wiring:
// what the block shows, what the dialog sends, that stopping confirms first,
// and that a members-only refusal lands in the form rather than a toast.

const state = vi.hoisted(() => ({
  data: null as IssueRecurrenceResponse | null,
  set: vi.fn(),
  clear: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("../../navigation", () => ({
  AppLink: ({ href, children, ...rest }: { href: string; children?: React.ReactNode }) => (
    <a href={href} {...rest}>
      {children}
    </a>
  ),
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));
vi.mock("@multica/core/autopilots", () => ({
  cronPreviewOptions: (_ws: string, expr: string, tz: string) => ({
    queryKey: ["cron-preview", expr, tz],
    queryFn: async () => ({ next_runs: ["2026-09-14T07:00:00Z"] }),
  }),
}));
vi.mock("@multica/core/recurrence", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/recurrence")>()),
  issueRecurrenceOptions: () => ({ queryKey: ["recurrence"], queryFn: async () => state.data }),
  useSetIssueRecurrence: () => ({ mutate: state.set, isPending: false }),
  useClearIssueRecurrence: () => ({ mutate: state.clear, isPending: false }),
}));

import { RecurrenceSection } from "./recurrence-section";

const payload = (over: Partial<IssueRecurrenceResponse> = {}): IssueRecurrenceResponse => ({
  recurrence: {
    id: "r-1",
    issue_id: "i-src",
    cron_expression: "0 9 * * 1",
    timezone: "Europe/Paris",
    mode: "schedule",
    enabled: true,
    next_run_at: "2026-09-14T07:00:00Z",
    last_occurrence_id: "i-2",
    occurrence_count: 2,
    created_by_type: "member",
    created_by_id: "u-1",
    created_at: "2026-09-01T10:00:00Z",
    updated_at: "2026-09-01T10:00:00Z",
  },
  source: { id: "i-src", identifier: "ONE-12", title: "Weekly review" },
  occurrences: [
    {
      id: "i-2",
      identifier: "ONE-19",
      title: "Weekly review",
      status: "todo",
      created_at: "2026-09-07T07:00:00Z",
      due_date: "2026-09-11",
    },
    {
      id: "i-src",
      identifier: "ONE-12",
      title: "Weekly review",
      status: "done",
      created_at: "2026-09-01T10:00:00Z",
      due_date: null,
    },
  ],
  next_runs: ["2026-09-14T07:00:00Z", "2026-09-21T07:00:00Z", "2026-09-28T07:00:00Z"],
  ...over,
});

function render(issueId = "i-src") {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <RecurrenceSection issueId={issueId} />
    </QueryClientProvider>,
  );
}

/** The body the last set-mutation was called with. */
function sentBody(): Record<string, unknown> {
  const [input] = state.set.mock.calls.at(-1) as [Record<string, unknown>];
  return input;
}

beforeAll(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-09-10T18:00:00Z"));
});
afterAll(() => vi.useRealTimers());

describe("RecurrenceSection", () => {
  beforeEach(() => {
    cleanup();
    state.data = null;
    state.set.mockReset();
    state.clear.mockReset();
  });

  it("says the issue happens once, and offers to make it recurring", async () => {
    render();
    expect(await screen.findByText(en.recurrence.empty)).toBeTruthy();
    expect(screen.getByRole("button", { name: en.recurrence.make_recurring })).toBeTruthy();
  });

  it("sends weekly Monday 09:00 Paris as a schedule rule", async () => {
    render();
    fireEvent.click(await screen.findByRole("button", { name: en.recurrence.make_recurring }));
    fireEvent.click(await screen.findByRole("radio", { name: en.recurrence.dialog.preset.weekly }));
    fireEvent.change(screen.getByLabelText(en.recurrence.dialog.time_label), {
      target: { value: "09:00" },
    });
    fireEvent.change(screen.getByLabelText(en.recurrence.dialog.timezone_label), {
      target: { value: "Europe/Paris" },
    });
    fireEvent.click(screen.getByRole("button", { name: en.recurrence.dialog.confirm }));

    await waitFor(() => expect(state.set).toHaveBeenCalled());
    expect(sentBody()).toEqual({
      cron_expression: "0 9 * * 1",
      timezone: "Europe/Paris",
      mode: "schedule",
      enabled: true,
    });
  });

  it("sends mode on_close with no cron when the next one follows the close", async () => {
    render();
    fireEvent.click(await screen.findByRole("button", { name: en.recurrence.make_recurring }));
    fireEvent.click(
      await screen.findByRole("radio", { name: en.recurrence.dialog.preset.on_close }),
    );
    fireEvent.click(screen.getByRole("button", { name: en.recurrence.dialog.confirm }));

    await waitFor(() => expect(state.set).toHaveBeenCalled());
    const body = sentBody();
    expect(body["mode"]).toBe("on_close");
    expect(body).not.toHaveProperty("cron_expression");
  });

  it("renders the saved rule as a sentence, with its next runs", async () => {
    state.data = payload();
    render();
    expect((await screen.findByTestId("recurrence-sentence")).textContent).toBe(
      "Every Monday at 09:00 (Europe/Paris)",
    );
    // Relative reads at a glance; the exact instant is one hover away.
    const runs = screen.getAllByTitle(/2026/);
    expect(runs.length).toBeGreaterThanOrEqual(3);
    expect(runs[0]?.textContent).toMatch(/^in /);
  });

  it("lists the series, marking the source, each linking to its issue", async () => {
    state.data = payload();
    render();
    const rows = await screen.findAllByTestId("recurrence-occurrence");
    expect(rows).toHaveLength(2);
    expect(rows[0]?.textContent).toContain("ONE-19");
    expect(rows[0]?.querySelector("a")?.getAttribute("href")).toBe("/acme/issues/i-2");
    expect(rows[1]?.textContent).toContain(en.recurrence.source);
  });

  // Regression: due_date ("2026-09-11", a date-only server string) was
  // interpolated verbatim into t() — "due 2026-09-11" in every locale —
  // instead of formatted like occurrence_created a few lines above.
  it("formats the due date instead of interpolating the raw ISO string", async () => {
    state.data = payload();
    render();
    const rows = await screen.findAllByTestId("recurrence-occurrence");
    expect(rows[0]?.textContent).not.toContain("2026-09-11");
    expect(rows[0]?.textContent).toContain("Sep 11");
  });

  it("keeps the rule and flips only `enabled` when the switch is turned off", async () => {
    state.data = payload();
    render();
    fireEvent.click(await screen.findByRole("switch", { name: en.recurrence.active }));
    await waitFor(() => expect(state.set).toHaveBeenCalled());
    expect(sentBody()).toEqual({
      cron_expression: "0 9 * * 1",
      timezone: "Europe/Paris",
      mode: "schedule",
      enabled: false,
    });
  });

  it("confirms before stopping, and only then calls the server", async () => {
    state.data = payload();
    render();
    fireEvent.click(await screen.findByRole("button", { name: en.recurrence.stop }));
    expect(await screen.findByText(en.recurrence.stop_dialog.title)).toBeTruthy();
    expect(state.clear).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: en.recurrence.stop_dialog.confirm }));
    await waitFor(() => expect(state.clear).toHaveBeenCalled());
  });

  it("tells an occurrence which series it belongs to, and links to the source", async () => {
    state.data = payload();
    render("i-2");
    const line = await screen.findByTestId("recurrence-series-line");
    expect(line.textContent).toContain("Part of the series ONE-12");
    expect(line.textContent).toContain("next on");
    expect(line.querySelector("a")?.getAttribute("href")).toBe("/acme/issues/i-src");
    // The rule is shared, so the same controls are offered from here.
    expect(screen.getByRole("button", { name: en.recurrence.stop })).toBeTruthy();
  });

  it("puts the members-only refusal in the form, not in a toast", async () => {
    state.set.mockImplementation(
      (_input: unknown, opts: { onError?: (e: unknown) => void }) =>
        opts.onError?.(new ApiError("only a member sets a recurrence", 403, "Forbidden")),
    );
    render();
    fireEvent.click(await screen.findByRole("button", { name: en.recurrence.make_recurring }));
    fireEvent.click(screen.getByRole("button", { name: en.recurrence.dialog.confirm }));

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toBe(en.recurrence.forbidden);
  });

  it("surfaces a rejected cron expression inline", async () => {
    state.set.mockImplementation(
      (_input: unknown, opts: { onError?: (e: unknown) => void }) =>
        opts.onError?.(new ApiError("invalid cron_expression: bad", 400, "Bad Request")),
    );
    render();
    fireEvent.click(await screen.findByRole("button", { name: en.recurrence.make_recurring }));
    fireEvent.click(await screen.findByRole("radio", { name: en.recurrence.dialog.preset.custom }));
    fireEvent.change(screen.getByLabelText(en.recurrence.dialog.cron_label), {
      target: { value: "nope" },
    });
    fireEvent.click(screen.getByRole("button", { name: en.recurrence.dialog.confirm }));

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("invalid cron_expression");
  });
});
