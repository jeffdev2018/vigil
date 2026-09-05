// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { RunReplay, ReplayEvent } from "@multica/core/issues/run-replay";
import { renderWithI18n } from "../../test/i18n";

// Pure helpers and client parsing: packages/core/issues/run-replay.test.ts.

const state = vi.hoisted(() => ({
  replay: null as RunReplay | null,
  resume: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/issues/run-replay", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/run-replay")>()),
  taskReplayOptions: (_ws: string, taskId: string) => ({ queryKey: ["replay", taskId], queryFn: async () => state.replay }),
  useResumeTaskReplay: () => ({ mutate: state.resume, isPending: false }),
}));

import { RunReplayDialog } from "./run-replay-dialog";

const ev = (seq: number, kind: string, over: Partial<ReplayEvent> = {}): ReplayEvent => ({
  seq, at: "2026-09-05T10:00:00Z", kind, actor: { type: "agent", id: "a1", name: "Builder" }, title: kind, text: "", data: {}, source: "task_message", source_id: String(seq), prev_hash: "", hash: "hash" + seq + "0000000000", ...over,
});

const replay = (over: Partial<RunReplay> = {}): RunReplay => ({
  run: { id: "t1", issue_id: "i1", agent_id: "a1", agent_name: "Builder", status: "completed", trust_mode: "propose", effect_mode: "apply", model: "", created_at: null, started_at: null, completed_at: null, links: [{ relation: "retry_of", task_id: "t0", agent_id: "a1", agent_name: "Builder" }] },
  events: [ev(0, "text", { title: "Agent says", text: "Starting" }), ev(1, "tool_use", { title: "Tool call: read_file", data: { tool: "read_file" } }), ev(2, "effect", { title: "Effect: issue_status" })],
  total: 3, next_cursor: null, head_hash: "hash20000000000", cost: { input_tokens: 1200, output_tokens: 300, cost_usd_ticks: 25_000_000_000 },
  sealed: { events: 3, head_hash: "hash20000000000", sealed_at: "2026-09-05T10:05:00Z", verified: true }, ...over,
});

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <RunReplayDialog taskId="t1" open onOpenChange={() => {}} />
    </QueryClientProvider>,
  );
}

describe("RunReplayDialog", () => {
  beforeEach(() => {
    state.replay = replay();
    state.resume.mockReset();
  });

  it("opens on the last event, scrubs back through the chain and counts what happened so far", async () => {
    render();
    const card = await screen.findByTestId("replay-event");
    expect(card.getAttribute("data-kind")).toBe("effect");
    expect(screen.getByTestId("replay-seal").getAttribute("data-state")).toBe("verified");
    expect(screen.getByText("3 events")).toBeTruthy();
    expect(screen.getByText(/\$2\.50/)).toBeTruthy();
    fireEvent.change(screen.getByRole("slider"), { target: { value: "1" } });
    expect(screen.getByTestId("replay-event").getAttribute("data-kind")).toBe("tool_use");
    expect(screen.getByText("Tool call: read_file")).toBeTruthy();
    expect(screen.getByTestId("replay-so-far").textContent).toContain("1 tool calls · 0 effects");
    fireEvent.click(screen.getByText("Previous"));
    expect(screen.getByText("Starting")).toBeTruthy();
    expect(screen.getByText("Retry of · Builder")).toBeTruthy();
  });

  it("resumes a finished run from the current event with a new instruction", async () => {
    render();
    await screen.findByTestId("replay-event");
    fireEvent.change(screen.getByRole("slider"), { target: { value: "1" } });
    fireEvent.change(screen.getByPlaceholderText("What should the next run do differently from here?"), { target: { value: "Read the other file" } });
    fireEvent.click(screen.getByText("Start a new run from here"));
    expect(state.resume).toHaveBeenCalledWith({ seq: 1, instruction: "Read the other file" }, expect.anything());
  });

  it("flags a broken seal and hides the resume form while the run is live", async () => {
    state.replay = replay({ run: { ...replay().run, status: "running" }, sealed: { events: 3, head_hash: "other", sealed_at: "x", verified: false } });
    render();
    await screen.findByTestId("replay-event");
    expect(screen.getByTestId("replay-seal").getAttribute("data-state")).toBe("broken");
    expect(screen.queryByTestId("replay-resume")).toBeNull();
  });
});
