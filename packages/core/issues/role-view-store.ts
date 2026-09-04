"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../platform/workspace-storage";
import { defaultStorage } from "../platform/storage";

// Role views (K32): how the issue page's read-only blocks are recomposed.
// A display preference, not a member role; persisted per workspace.

export const ISSUE_ROLE_VIEWS = ["full", "pm", "qa", "cto"] as const;
export type IssueRoleView = (typeof ISSUE_ROLE_VIEWS)[number];

export function isIssueRoleView(value: unknown): value is IssueRoleView {
  return typeof value === "string" && (ISSUE_ROLE_VIEWS as readonly string[]).includes(value);
}

interface IssueRoleViewState {
  view: IssueRoleView;
  setView: (view: IssueRoleView) => void;
}

export const useIssueRoleViewStore = create<IssueRoleViewState>()(
  persist(
    (set) => ({
      view: "full",
      setView: (view) => set({ view: isIssueRoleView(view) ? view : "full" }),
    }),
    {
      name: "multica_issue_role_view",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      partialize: (state) => ({ view: state.view }),
      merge: (persisted, current) => {
        const p = persisted as Partial<IssueRoleViewState> | undefined;
        return { ...current, view: isIssueRoleView(p?.view) ? p.view : "full" };
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useIssueRoleViewStore.persist.rehydrate());
