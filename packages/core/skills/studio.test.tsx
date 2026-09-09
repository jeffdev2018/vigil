// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { useRunSkillTest, studioNotebook, useSkillStudioStore } from "./studio";

const api = vi.hoisted(() => ({ getSkill: vi.fn(), createChatSession: vi.fn(), sendChatMessage: vi.fn() }));
vi.mock("../api", () => ({ api }));
const skill = { id: "skill", workspace_id: "ws", name: "Review", content: "Review the evidence.", updated_at: "2026-09-08", files: [{ id: "f", skill_id: "skill", path: "checklist.md", content: "Check sources." }] };
const request = { skillId: "skill", expectedUpdatedAt: "2026-09-08", agentId: "agent", inputName: "Case one", input: "Test this claim." };
function wrapper({ children }: { children: ReactNode }) { return <QueryClientProvider client={new QueryClient({ defaultOptions: { mutations: { retry: false } } })}>{children}</QueryClientProvider>; }
beforeEach(() => { vi.resetAllMocks(); useSkillStudioStore.getState().setDraft({ notebooks: {} }); api.getSkill.mockResolvedValue(skill); api.createChatSession.mockResolvedValue({ id: "session-1" }); api.sendChatMessage.mockResolvedValue({ task_id: "task-1" }); });
describe("skill test execution", () => {
  it("creates separate conversations with the saved skill and supporting files; records no fabricated output", async () => {
    api.createChatSession.mockResolvedValueOnce({ id: "session-1" }).mockResolvedValueOnce({ id: "session-2" });
    const { result } = renderHook(() => useRunSkillTest("ws", "book"), { wrapper });
    await act(async () => { await result.current.mutateAsync(request); await result.current.mutateAsync(request); });
    expect(api.sendChatMessage.mock.calls[0]).toEqual(["session-1", expect.stringContaining("Check sources.")]);
    expect(api.sendChatMessage.mock.calls[1]).toEqual(["session-2", expect.stringContaining("Test this claim.")]);
    expect(studioNotebook("book").runs.map(r => r.sessionId)).toEqual(["session-2", "session-1"]);
    expect(studioNotebook("book").runs[0]).not.toHaveProperty("output");
  });
  it("rejects a changed or malformed saved skill before creating a conversation", async () => {
    const { result } = renderHook(() => useRunSkillTest("ws", "book"), { wrapper });
    await act(async () => { await expect(result.current.mutateAsync({ ...request, expectedUpdatedAt: "old" })).rejects.toThrow("changed"); });
    api.getSkill.mockResolvedValue({ id: "skill" });
    await act(async () => { await expect(result.current.mutateAsync(request)).rejects.toThrow("Invalid saved skill"); });
    expect(api.createChatSession).not.toHaveBeenCalled();
    expect(studioNotebook("book").runs).toEqual([]);
  });
  it("does not report a run as started when sending fails", async () => {
    api.sendChatMessage.mockRejectedValue(new Error("Agent unavailable"));
    const { result } = renderHook(() => useRunSkillTest("ws", "book"), { wrapper });
    await act(async () => { await expect(result.current.mutateAsync(request)).rejects.toThrow("Agent unavailable"); });
    expect(studioNotebook("book").runs).toEqual([]);
  });
});
