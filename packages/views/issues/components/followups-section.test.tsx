// @vitest-environment jsdom
import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { IssueFollowupsResponse } from "@multica/core/types";
import { ApiError } from "@multica/core/api";
import en from "../../locales/en/issues.json";
import { renderWithI18n } from "../../test/i18n";

// Client parsing, the quick-choice instants and the cache updater are covered
// in packages/core/followups/followups.test.ts. This suite keeps the wiring:
// what the list shows, what a quick choice sends, that cancel confirms first,
// and that a refused budget lands in the form rather than a toast.

const state = vi.hoisted(() => ({
  data: null as IssueFollowupsResponse | null,
  schedule: vi.fn(),
  cancel: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({
    queryKey: ["agents"],
    queryFn: async () => [{ id: "a-1", name: "Ada", archived_at: null }],
  }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (_type: string, id: string) => (id === "u-1" ? "Jeff" : id) }),
}));
vi.mock("@multica/core/followups", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/followups")>()),
  issueFollowupsOptions: () => ({ queryKey: ["followups"], queryFn: async () => state.data }),
  useScheduleFollowup: () => ({ mutate: state.schedule, isPending: false }),
  useCancelFollowup: () => ({ mutate: state.cancel, isPending: false }),
}));

import { FollowupsSection } from "./followups-section";

const response = (over: Partial<IssueFollowupsResponse> = {}): IssueFollowupsResponse => ({
  followups: [
    {
      id: "f-1",
      issue_id: "i-1",
      agent_id: "a-1",
      agent_name: "Ada",
      fires_at: "2026-09-11T09:00:00Z",
      note: "Check whether staging is green",
      scheduled_by_type: "member",
      scheduled_by_id: "u-1",
      created_at: "2026-09-10T18:00:00Z",
    },
  ],
  budget: { max_per_agent_per_day: 20, max_per_workspace_per_day: 200 },
  ...over,
});

function render(
  issue: { assignee_type: "agent" | "member" | null; assignee_id: string | null } = {
    assignee_type: "agent",
    assignee_id: "a-1",
  },
) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <FollowupsSection issueId="i-1" issue={issue} />
    </QueryClientProvider>,
  );
}

beforeAll(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-09-10T18:00:00Z"));
});
afterAll(() => vi.useRealTimers());

describe("FollowupsSection", () => {
  beforeEach(() => {
    cleanup();
    state.data = response();
    state.schedule.mockReset();
    state.cancel.mockReset();
  });

  it("lists a pending wake-up with its countdown, agent, note and who filed it", async () => {
    render();
    const row = await screen.findByTestId("followup-row");
    // 15h out from the frozen clock.
    expect(row.textContent).toContain("15h 00m");
    expect(row.textContent).toContain("Ada");
    expect(row.textContent).toContain("Check whether staging is green");
    expect(row.textContent).toContain("Jeff");
    // The exact instant is one hover away.
    expect(row.querySelector("[title]")?.getAttribute("title")).toBeTruthy();
    expect(screen.getByText(/At most 20 per agent/)).toBeTruthy();
  });

  it("says so in one line when nothing is scheduled", async () => {
    state.data = response({ followups: [] });
    render();
    expect(await screen.findByText(en.followups.empty)).toBeTruthy();
  });

  it("sends the quick choice's instant, note and no agent when the issue has one", async () => {
    render();
    fireEvent.click(await screen.findByRole("button", { name: en.followups.schedule }));

    fireEvent.click(await screen.findByRole("button", { name: en.followups.quick.tomorrow_9 }));
    fireEvent.change(screen.getByLabelText(en.followups.dialog.note_label), {
      target: { value: "Check staging" },
    });
    fireEvent.click(screen.getByRole("button", { name: en.followups.dialog.confirm }));

    await waitFor(() => expect(state.schedule).toHaveBeenCalled());
    const [input] = state.schedule.mock.calls[0] as [
      { when: string; note?: string; agent_id?: string },
    ];
    expect(input.note).toBe("Check staging");
    expect(input.agent_id).toBeUndefined();
    const at = new Date(input.when);
    expect(at.getHours()).toBe(9);
    expect(at.getTime()).toBeGreaterThan(Date.now());
  });

  it("asks which agent wakes up when the issue has no agent assignee", async () => {
    render({ assignee_type: "member", assignee_id: "m-1" });
    fireEvent.click(await screen.findByRole("button", { name: en.followups.schedule }));
    expect(await screen.findByLabelText(en.followups.dialog.agent_label)).toBeTruthy();
    // Nothing to send until an agent is named.
    expect(
      screen.getByRole("button", { name: en.followups.dialog.confirm }).hasAttribute("disabled"),
    ).toBe(true);
  });

  it("puts a refused budget in the form, not in a toast", async () => {
    state.schedule.mockImplementation(
      (
        _input: unknown,
        opts: { onError?: (e: unknown) => void },
      ) =>
        opts.onError?.(
          new ApiError("follow-up budget reached: at most 20 per agent per day", 429, "Too Many Requests"),
        ),
    );
    render();
    fireEvent.click(await screen.findByRole("button", { name: en.followups.schedule }));
    fireEvent.click(screen.getByRole("button", { name: en.followups.dialog.confirm }));

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("at most 20 per agent per day");
  });

  it("confirms before cancelling, and only then calls the server", async () => {
    render();
    fireEvent.click(await screen.findByRole("button", { name: en.followups.cancel }));
    expect(await screen.findByText(en.followups.cancel_dialog.title)).toBeTruthy();
    expect(state.cancel).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: en.followups.cancel_dialog.confirm }));
    await waitFor(() => expect(state.cancel).toHaveBeenCalledWith("f-1", expect.anything()));
  });
});
