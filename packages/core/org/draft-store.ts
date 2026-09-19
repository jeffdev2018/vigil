import { createDraftStore } from "../drafts/create-draft-store";
import type { OrgModel } from "../types";

export interface OrgEditorDraft {
  revision: number;
  form: { name: string; owner_id: string; dissolve_at: string; end_condition: string; budget: string; model: OrgModel; definition: string };
}

export const useOrgDraftStore = createDraftStore<{
  selectedId: string | null;
  edits: Record<string, OrgEditorDraft>;
}>({
  storageKey: "org-drafts",
  emptyData: { selectedId: null, edits: {} },
  hasMeaningful: data => Object.keys(data.edits).length > 0,
});

export function saveOrgDraft(id: string, draft: OrgEditorDraft) {
  const store = useOrgDraftStore.getState();
  store.setDraft({ edits: { ...store.draft.edits, [id]: draft } });
}

export function clearOrgDraft(id: string) {
  const store = useOrgDraftStore.getState();
  const edits = { ...store.draft.edits };
  delete edits[id];
  store.setDraft({ edits });
}

export const useOrgWizardDraftStore = createDraftStore<{
  step: number;
  projectId: string;
  purpose: string;
  decider: import("./templates").OrgDecider | null;
  teamShape: import("./templates").OrgTeamShape | null;
  hasEnd: boolean | null;
  compete: boolean | null;
  chosenModel: import("../types").OrgModel | null;
  placement: Record<string, string>;
}>({
  storageKey: "org-wizard-draft",
  emptyData: { step: 1, projectId: "", purpose: "", decider: null, teamShape: null, hasEnd: null, compete: null, chosenModel: null, placement: {} },
  hasMeaningful: data => data.purpose.trim().length > 0 || data.projectId !== "",
});
