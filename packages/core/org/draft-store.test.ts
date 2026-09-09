// @vitest-environment jsdom
import { expect, it } from "vitest";
import { setCurrentWorkspace } from "../platform/workspace-storage";
import { clearOrgDraft, saveOrgDraft, useOrgDraftStore } from "./draft-store";

it("persists edits in their own workspace and restores them after a workspace switch", async () => {
  setCurrentWorkspace("org-draft-test-a", "a");
  await Promise.resolve();
  useOrgDraftStore.getState().clearDraft();
  const draft = { revision: 4, form: { name: "Draft team", owner_id: "", dissolve_at: "", end_condition: "", budget: "0", definition: "{}" } };
  saveOrgDraft("org", draft);
  setCurrentWorkspace("org-draft-test-b", "b");
  await Promise.resolve();
  expect(useOrgDraftStore.getState().draft.edits.org).toBeUndefined();
  setCurrentWorkspace("org-draft-test-a", "a");
  await Promise.resolve();
  expect(useOrgDraftStore.getState().draft.edits.org).toEqual(draft);
  clearOrgDraft("org");
  expect(useOrgDraftStore.getState().draft.edits.org).toBeUndefined();
  useOrgDraftStore.getState().clearDraft();
  setCurrentWorkspace(null, null);
});
