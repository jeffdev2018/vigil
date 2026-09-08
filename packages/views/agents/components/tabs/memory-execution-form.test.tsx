// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { AgentMemory } from "@multica/core/types";
import agents from "../../../locales/en/agents.json";
const mocks = vi.hoisted(() => ({ config: vi.fn(), start: vi.fn() }));
vi.mock("@multica/core/api", async (original) => ({ ...await original<typeof import("@multica/core/api")>(), api: { getMemoryExecutionConfig: mocks.config, startMemoryExecution: mocks.start } }));
import { MemoryExecutionForm } from "./memory-execution-form";
afterEach(() => { cleanup(); vi.clearAllMocks(); });
const memory: AgentMemory = { id: "memory", agent_id: "agent", content: "Check assumptions", source: "manual", state: "approved", source_task_id: null, source_issue_id: null, created_at: "", updated_at: "", revision: 1, status: "pending" };
function mount() {
  const onStarted = vi.fn();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  render(<I18nProvider locale="en" resources={{ en: { agents } }}><QueryClientProvider client={client}><MemoryExecutionForm wsId="ws" agentId="agent" memory={memory} disabled={false} onStarted={onStarted} /></QueryClientProvider></I18nProvider>);
  return onStarted;
}
it("launches frozen replay/holdout cases and reuses the request identity after an uncertain failure", async () => {
  mocks.config.mockResolvedValue({ runtime_id: "rt", provider: "claude", model: "fixture", effort: "low", config_hash: "a".repeat(64), max_cases: 8, timeout_seconds: 60, check_modes: ["exact", "json", "javascript"] });
  mocks.start.mockRejectedValueOnce(new Error("lost response")).mockResolvedValue({ id: "report" });
  const onStarted = mount(); const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Run a connected comparison" }));
  await screen.findByText("claude · fixture · low");
  expect((screen.getByRole("button", { name: "Start four runs" }) as HTMLButtonElement).disabled).toBe(true);
  await user.selectOptions(screen.getAllByLabelText("Check type")[0]!, "json");
  const prompts = screen.getAllByLabelText("Test prompt"), answers = screen.getAllByLabelText("Expected answer");
  await user.type(prompts[0]!, "Original question"); await user.type(prompts[1]!, "Independent question");
  await user.type(answers[0]!, '"answer"'); await user.type(answers[1]!, "answer");
  await user.click(screen.getByRole("button", { name: "Start four runs" }));
  await screen.findByRole("alert");
  await user.click(screen.getByRole("button", { name: "Start four runs" }));
  await waitFor(() => expect(onStarted).toHaveBeenCalledWith("report"));
  const first = mocks.start.mock.calls[0]![2], retry = mocks.start.mock.calls[1]![2];
  expect(retry.request_id).toBe(first.request_id);
  expect(first).toMatchObject({ expected_revision: 1, config_hash: "a".repeat(64), cases: [{ split: "replay", prompt: "Original question", expected: '"answer"', check: "json" }, { split: "holdout", prompt: "Independent question", expected: "answer" }] });
});
it("shows configuration failure without offering an executable launch", async () => {
  mocks.config.mockRejectedValue(new Error("offline")); mount();
  await userEvent.click(screen.getByRole("button", { name: "Run a connected comparison" }));
  await screen.findByRole("alert"); expect(screen.queryByRole("button", { name: "Start four runs" })).toBeNull(); expect(mocks.start).not.toHaveBeenCalled();
});
