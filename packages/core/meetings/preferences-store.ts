"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";

/**
 * Durable client preferences for meetings. Separate from the recorder store,
 * which is deliberately NOT persisted (a MediaRecorder dies with the page).
 *
 * `detectMeetings` drives the desktop shell's ambient microphone watcher. It is
 * on by default — the feature only prompts, it never records on its own — but
 * a watcher that cannot be turned off is a watcher some people will not run at
 * all. Web ignores it: there is nothing there to watch the microphone with.
 */
interface MeetingPreferencesState {
  detectMeetings: boolean;
  setDetectMeetings: (value: boolean) => void;
}

export const useMeetingPreferencesStore = create<MeetingPreferencesState>()(
  persist(
    (set) => ({
      detectMeetings: true,
      setDetectMeetings: (detectMeetings) => set({ detectMeetings }),
    }),
    {
      name: "multica_meeting_preferences",
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => ({ detectMeetings: state.detectMeetings }),
    },
  ),
);
