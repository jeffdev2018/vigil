// @vitest-environment jsdom
import { beforeEach, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Skill } from "@multica/core/types";
import { useSkillStudioStore } from "@multica/core/skills/studio";
import { renderWithI18n } from "../../test/i18n";
import { SkillStudio } from "./skill-studio";
const api = vi.hoisted(() => ({ getSkill: vi.fn(), createChatSession: vi.fn(), sendChatMessage: vi.fn(), listChatMessages: vi.fn(), getPendingChatTask: vi.fn() }));
vi.mock("@multica/core/api", () => ({ api }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ chat: () => "/ws/chat" }) }));
vi.mock("../../navigation", () => ({ AppLink: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a> }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/auth", () => ({ useAuthStore: (select: (s: { user: { id: string } }) => unknown) => select({ user: { id: "owner" } }) }));
vi.mock("@multica/core/workspace/queries", () => ({ agentListOptions: () => ({ queryKey: ["agents"], queryFn: async () => [{ id: "agent", name: "Reviewer", runtime_id: "runtime" }] }) }));
const skill: Skill = { id: "skill", workspace_id: "ws", name: "Review", description: "", content: "Review the sources", config: {}, created_by: null, created_at: "2026-09-08", updated_at: "2026-09-08", status: "published", files: [] };
function renderStudio(dirty: boolean) { return renderWithI18n(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })}><SkillStudio skill={skill} dirty={dirty} /></QueryClientProvider>); }
beforeEach(() => { vi.resetAllMocks(); useSkillStudioStore.getState().setDraft({ notebooks: {} }); api.getSkill.mockResolvedValue(skill); api.createChatSession.mockResolvedValue({ id: "session" }); api.sendChatMessage.mockResolvedValue({ task_id: "task" }); api.listChatMessages.mockResolvedValue([{ id: "response", role: "assistant", content: "The source is missing." }]); api.getPendingChatTask.mockResolvedValue({ task_id: null }); });
it("keeps unsaved skill changes from starting a test", async () => {
  renderStudio(true);
  await screen.findByRole("option", { name: "Reviewer" });
  fireEvent.change(screen.getByLabelText("Test input"), { target: { value: "Check this claim" } });
  fireEvent.change(screen.getByLabelText("Test agent"), { target: { value: "agent" } });
  expect(screen.getByRole("button", { name: "Run test" })).toBeDisabled();
  expect(api.createChatSession).not.toHaveBeenCalled();
});
it("runs a saved input and displays the actual conversation response", async () => {
  renderStudio(false);
  await screen.findByRole("option", { name: "Reviewer" });
  fireEvent.change(screen.getByLabelText("Test input"), { target: { value: "Check this claim" } });
  fireEvent.change(screen.getByLabelText("Test agent"), { target: { value: "agent" } });
  fireEvent.click(screen.getByRole("button", { name: "Run test" }));
  await waitFor(() => expect(api.sendChatMessage).toHaveBeenCalledWith("session", expect.stringContaining("Check this claim")));
  expect(await screen.findByText("The source is missing.")).toBeVisible();
});
