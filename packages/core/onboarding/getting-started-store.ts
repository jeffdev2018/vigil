"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";

/**
 * Durable per-workspace dismissal of the getting-started checklist card
 * (OS plan, chantier 5). Keyed by workspace id, like `useRecentIssuesStore`,
 * so a member who dismisses it in one workspace still sees it in another.
 *
 * The card also auto-hides once the checklist reports `complete: true` —
 * this store only covers the earlier, explicit "not now" exit.
 */
interface GettingStartedStoreState {
  dismissedByWorkspace: Record<string, boolean>;
  dismiss: (wsId: string) => void;
}

export const useGettingStartedStore = create<GettingStartedStoreState>()(
  persist(
    (set) => ({
      dismissedByWorkspace: {},
      dismiss: (wsId) =>
        set((state) => ({
          dismissedByWorkspace: { ...state.dismissedByWorkspace, [wsId]: true },
        })),
    }),
    {
      name: "multica_getting_started_dismissed",
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => ({ dismissedByWorkspace: state.dismissedByWorkspace }),
    },
  ),
);

export function selectGettingStartedDismissed(wsId: string | null) {
  return (state: GettingStartedStoreState) =>
    wsId ? (state.dismissedByWorkspace[wsId] ?? false) : false;
}
