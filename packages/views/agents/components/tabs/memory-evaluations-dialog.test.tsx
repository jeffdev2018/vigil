// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { AgentMemory } from "@multica/core/types";
import agents from "../../../locales/en/agents.json";
const mocks = vi.hoisted(() => ({ list: vi.fn(), get: vi.fn(), update: vi.fn(), upload: vi.fn(), remove: vi.fn() }));
vi.mock("@multica/core/api", async (original) => ({ ...await original<typeof import("@multica/core/api")>(), api: { listAgentMemoryEvaluations: mocks.list, getAgentMemoryEvaluation: mocks.get, updateAgentMemory: mocks.update, importAgentMemoryEvaluation: mocks.upload, deleteAgentMemoryEvaluation: mocks.remove } }));
import { MemoryEvaluationsDialog } from "./memory-evaluations-dialog";

afterEach(() => { cleanup(); Object.values(mocks).forEach((fn) => fn.mockReset()); });
const memory: AgentMemory = { id: "memory", agent_id: "agent", content: "Check assumptions", source: "manual", state: "approved", source_task_id: null, source_issue_id: null, created_at: "", updated_at: "", revision: 1, status: "pending" };
vi.mock("./memory-execution-form", () => ({ MemoryExecutionForm: () => null }));
const outcome = { status: "passed", duration_ms: 12, artifact: "fixed output", diagnostic: "checks passed", runtime: { provider: "claude", requested_model: "fixture", tool_calls: 1, usage: null } };
const report = { candidate: { content: memory.content, revision: 1 }, suite: { image: "sha256:fixture", worker: ["/worker"], verifier: ["/verifier"] }, cases: [{ id: "unseen-case", split: "holdout", input_hash: "input-fingerprint", checks_hash: "check-fingerprint", baseline: { ...outcome, status: "failed" }, candidate: outcome }] };
const saved = { id: "evaluation", memory_id: memory.id, revision: 1, eligible: true, adopted_revision: null, total: 2, baseline_passed: 0, candidate_passed: 2, regressions: 0, errors: 0, created_at: "2026-09-06T12:00:00Z", report };
function mount(value = memory) {
  const close = vi.fn();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  render(<I18nProvider locale="en" resources={{ en: { agents } }}><QueryClientProvider client={client}><MemoryEvaluationsDialog wsId="ws" agentId="agent" memory={value} onClose={close} /></QueryClientProvider></I18nProvider>);
  return close;
}
it("shows imported evidence and requires human review before adopting its exact revision", async () => {
  mocks.list.mockResolvedValue([saved]); mocks.get.mockResolvedValue(saved); mocks.update.mockResolvedValue({ ...memory, status: "active", revision: 2 });
  const close = mount(); const user = userEvent.setup();
  expect(await screen.findByText(/Without candidate: 0\/2 passed/)).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Inspect report" }));
  await screen.findByText(memory.content);
  const adopt = screen.getByRole("button", { name: "Adopt this evaluated revision" });
  expect((adopt as HTMLButtonElement).disabled).toBe(true);
  expect(screen.getByText(/Cost and human effort were not measured/)).toBeTruthy();
  expect(screen.getAllByText("Adapter observations (reported, not attested)")).toHaveLength(2);
  await user.click(screen.getByRole("checkbox")); await user.click(adopt);
  await waitFor(() => expect(mocks.update).toHaveBeenCalledWith("agent", "memory", { status: "active", expected_revision: 1, evaluation_id: "evaluation" }));
  await waitFor(() => expect(close).toHaveBeenCalledOnce());
});
it("keeps failed loads visible, retries, and does not offer adoption for an old revision", async () => {
  mocks.list.mockRejectedValueOnce(new Error("offline")).mockResolvedValue([saved]); mocks.get.mockResolvedValue(saved);
  mount({ ...memory, revision: 2 }); const user = userEvent.setup();
  await screen.findByRole("alert");
  await user.click(screen.getByRole("button", { name: "Try again" }));
  await user.click(await screen.findByRole("button", { name: "Inspect report" }));
  await screen.findByText(memory.content);
  expect(screen.queryByRole("button", { name: "Adopt this evaluated revision" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Remove report" }));
  expect(mocks.remove).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Confirm removal" }));
  await waitFor(() => expect(mocks.remove).toHaveBeenCalledWith("agent", "memory", "evaluation"));
});
