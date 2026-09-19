import { queryOptions, useMutation } from "@tanstack/react-query";
import { api } from "../api";
import { SkillSchema, ChatSessionSchema } from "../api/schemas";
import { parseWithFallback } from "../api/schema";
import type { ChatSession } from "../types";
import { createDraftStore } from "../drafts/create-draft-store";
import type { Skill } from "../types";

export interface StudioInput { id: string; name: string; text: string }
export interface StudioRunLink { sessionId: string; taskId: string; inputName: string; agentId: string; skillUpdatedAt: string; startedAt: string }
export interface StudioNotebook { inputs: StudioInput[]; runs: StudioRunLink[] }
const EMPTY: StudioNotebook = { inputs: [], runs: [] };
// User-authored inputs and navigation pointers only; results remain in Query/server state.
export const useSkillStudioStore = createDraftStore<{ notebooks: Record<string, StudioNotebook> }>({ storageKey: "multica:skills:studio", emptyData: { notebooks: {} }, hasMeaningful: d => Object.keys(d.notebooks).length > 0 });
export function studioNotebook(key: string): StudioNotebook { return useSkillStudioStore.getState().draft.notebooks[key] ?? EMPTY; }
export function saveStudioInput(key: string, input: StudioInput) {
  const store = useSkillStudioStore.getState(), book = studioNotebook(key);
  store.setDraft({ notebooks: { ...store.draft.notebooks, [key]: { ...book, inputs: [...book.inputs.filter(i => i.id !== input.id), input] } } });
}

/** The immutable, saved skill snapshot is recorded in the run's first message. */
export function skillStudioPrompt(skill: Skill, input: string): string {
  const files = skill.files.filter(f => f.path !== "SKILL.md").map(f => ({ path: f.path, content: f.content }));
  return `Skill test: ${skill.name}\nSaved version: ${skill.updated_at}\nApply the following saved skill to the test input. Follow your normal permissions, approval rules and budget. Return the result and explain any blockers; do not claim checks you did not perform.\n\n${JSON.stringify({ skill: { name: skill.name, instructions: skill.content, files }, input }, null, 2)}`;
}
export function useRunSkillTest(wsId: string, notebookKey: string) {
  return useMutation({ mutationFn: async (request: { skillId: string; expectedUpdatedAt: string; agentId: string; inputName: string; input: string }) => {
    if (!request.input.trim() || !request.agentId) throw new Error("Choose an agent and enter test input.");
    const skill = parseWithFallback<Skill | null>(await api.getSkill(request.skillId), SkillSchema, null, { endpoint: "GET skill for test" });
    if (!skill || skill.id !== request.skillId) throw new Error("Invalid saved skill response");
    if (skill.updated_at !== request.expectedUpdatedAt) throw new Error("The skill changed. Reload it before testing.");
    const prompt = skillStudioPrompt(skill, request.input);
    if (prompt.length > 100_000) throw new Error("This skill and input are too large for a test conversation.");
    const session = parseWithFallback<ChatSession | null>(await api.createChatSession({ agent_id: request.agentId, title: `${skill.name} · ${request.inputName}` }), ChatSessionSchema, null, { endpoint: "POST test conversation" });
    if (!session?.id) throw new Error("Invalid test conversation response");
    const sent = await api.sendChatMessage(session.id, prompt);
    const link: StudioRunLink = { sessionId: session.id, taskId: sent.task_id, inputName: request.inputName, agentId: request.agentId, skillUpdatedAt: skill.updated_at, startedAt: new Date().toISOString() };
    const store = useSkillStudioStore.getState(), book = studioNotebook(notebookKey);
    store.setDraft({ notebooks: { ...store.draft.notebooks, [notebookKey]: { ...book, runs: [link, ...book.runs] } } });
    return link;
  }, meta: { workspaceId: wsId } });
}
export function skillStudioResultOptions(wsId: string, sessionId: string | undefined) {
  return queryOptions({ queryKey: ["skill-studio", wsId, sessionId], enabled: !!sessionId, queryFn: async () => {
    const [messages, pending] = await Promise.all([api.listChatMessages(sessionId!), api.getPendingChatTask(sessionId!)]);
    return { messages, pending };
  }, refetchInterval: query => query.state.data?.pending.task_id || (!query.state.data?.messages.some(m => m.role === "assistant") && query.state.dataUpdateCount < 5) ? 2000 : false });
}
export function useCancelSkillTest() { return useMutation({ mutationFn: (taskId: string) => api.cancelTaskById(taskId) }); }
