/**
 * Draft state for the New Event modal
 * (`app/(app)/[workspace]/new-event.tsx`), OS plan chantier 19.
 *
 * Same rationale as `new-issue-draft-store.ts`: the participants picker
 * (`new-event-picker/participants.tsx`) is a sibling Stack screen with no
 * React parent-child relationship to the New Event modal, so it reads and
 * writes the draft through this store instead of props/callbacks.
 *
 * Workspace lifecycle: reset on workspace change is wired in
 * `app/(app)/[workspace]/_layout.tsx` via
 * `useNewEventDraftResetOnWorkspaceChange`, same call site as the other
 * new-*-draft stores.
 */
import { useEffect, useRef } from "react";
import { create } from "zustand";

export interface CalendarParticipantValue {
  type: "member" | "agent";
  id: string;
}

interface NewEventDraftState {
  title: string;
  location: string;
  allDay: boolean;
  startsAt: Date;
  endsAt: Date;
  participants: CalendarParticipantValue[];
  setTitle: (next: string) => void;
  setLocation: (next: string) => void;
  setAllDay: (next: boolean) => void;
  setStartsAt: (next: Date) => void;
  setEndsAt: (next: Date) => void;
  setParticipants: (next: CalendarParticipantValue[]) => void;
  reset: () => void;
}

function defaultWindow(): { startsAt: Date; endsAt: Date } {
  const startsAt = new Date();
  startsAt.setMinutes(0, 0, 0);
  startsAt.setHours(startsAt.getHours() + 1);
  const endsAt = new Date(startsAt);
  endsAt.setMinutes(endsAt.getMinutes() + 30);
  return { startsAt, endsAt };
}

const INITIAL: Pick<
  NewEventDraftState,
  "title" | "location" | "allDay" | "startsAt" | "endsAt" | "participants"
> = {
  title: "",
  location: "",
  allDay: false,
  ...defaultWindow(),
  participants: [],
};

export const useNewEventDraftStore = create<NewEventDraftState>((set) => ({
  ...INITIAL,
  setTitle: (next) => set({ title: next }),
  setLocation: (next) => set({ location: next }),
  setAllDay: (next) => set({ allDay: next }),
  setStartsAt: (next) => set({ startsAt: next }),
  setEndsAt: (next) => set({ endsAt: next }),
  setParticipants: (next) => set({ participants: next }),
  reset: () => set({ ...INITIAL, ...defaultWindow() }),
}));

/** Mirrors `useNewIssueDraftResetOnWorkspaceChange` — see that file for the
 *  first-mount-is-a-no-op rationale. */
export function useNewEventDraftResetOnWorkspaceChange(wsId: string | null) {
  const prevRef = useRef(wsId);
  useEffect(() => {
    if (prevRef.current !== wsId) {
      useNewEventDraftStore.getState().reset();
      prevRef.current = wsId;
    }
  }, [wsId]);
}
