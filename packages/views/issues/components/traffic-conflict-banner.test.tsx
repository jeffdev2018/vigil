// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { TrafficConflict } from "@multica/core/issues/traffic";
import { renderWithI18n } from "../../test/i18n";

// Parsing: packages/core/issues/traffic.test.ts.

const state = vi.hoisted(() => ({ conflicts: [] as TrafficConflict[], ignore: vi.fn(), pause: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/issues/run-control", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/run-control")>()),
  usePauseRun: () => ({ mutate: state.pause, isPending: false }),
}));
vi.mock("@multica/core/issues/traffic", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/traffic")>()),
  issueTrafficConflictsOptions: () => ({ queryKey: ["tc"], queryFn: async () => state.conflicts }),
  useIgnoreTrafficConflict: () => ({ mutate: state.ignore, isPending: false }),
}));

import { TrafficConflictBanner } from "./traffic-conflict-banner";

const conflict = (over: Partial<TrafficConflict>): TrafficConflict => ({
  id: "c1", task_id: "t1", kind: "human", paths: ["src/a.go"], other_task_id: null, handoff_packet_id: "p1", status: "active", created_at: "2026-09-04T10:00:00Z", resolved_at: null, ...over,
});

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <TrafficConflictBanner issueId="i1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.conflicts = [];
  state.ignore.mockReset();
  state.pause.mockReset();
});

describe("TrafficConflictBanner", () => {
  it("renders nothing without a conflict", async () => {
    const { container } = render();
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toBe("");
  });

  it("shows an active human conflict with its paths and offers pause and ignore", async () => {
    state.conflicts = [conflict({}), conflict({ id: "c2", kind: "agent", other_task_id: "abcdef0123", paths: ["docs/x.md"], status: "resolved", resolved_at: "2026-09-04T10:05:00Z" })];
    render();
    expect(await screen.findByText(/human is changing/)).toBeTruthy();
    expect(screen.getByText("src/a.go")).toBeTruthy();
    expect(screen.getAllByTestId("traffic-conflict")).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "Pause the run" }));
    expect(state.pause).toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Ignore" }));
    expect(state.ignore).toHaveBeenCalledWith("c1", expect.anything());
    fireEvent.click(screen.getByText("Show 1 settled conflict"));
    expect(screen.getByTestId("traffic-conflict-settled")).toBeTruthy();
    expect(screen.getByText(/abcdef01/)).toBeTruthy();
  });
});
