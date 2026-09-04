import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";

/**
 * Client state for voice interaction: one durable preference, plus the bridge
 * from one dictated message to the reply that answers it.
 *
 * The bridge has two steps on purpose. `dictating` records that a voice memo
 * landed in the composer; `speakNextReply` is what the first assistant reply
 * consumes to read itself aloud. Only an actual SEND turns the first into the
 * second (`armSpeakOnSend`).
 *
 * Arming at transcription instead — which is what the composer used to do —
 * armed a reply for a memo the user then deleted, edited away, or carried into
 * a different conversation: the flag outlived the composer it was typed in and
 * the next reply anywhere started talking.
 *
 * Anything else about speech (what is currently being spoken) lives in the
 * browser's SpeechSynthesis, not here.
 *
 * `readRepliesAloud` is the only durable thing here — a Settings preference,
 * persisted; the two flags above are ephemeral and never written.
 */
interface VoiceState {
  /**
   * Settings → Chat. When off, a dictated message behaves like a typed one and
   * nothing speaks by itself; the per-message Listen button still works.
   */
  readRepliesAloud: boolean;
  setReadRepliesAloud: (value: boolean) => void;
  /** True from the send of a dictated message until a reply consumes it. */
  speakNextReply: boolean;
  /** True while a transcribed memo sits unsent in the composer. */
  dictating: boolean;
  setSpeakNextReply: (value: boolean) => void;
  /** A voice memo's text just landed in the composer. */
  markDictated: () => void;
  /** A message was accepted: arm the reply only if this turn was dictated. */
  armSpeakOnSend: () => void;
  /** The composer went away, or moved to another conversation. */
  resetSpeech: () => void;
}

export const useVoiceStore = create<VoiceState>()(
  persist(
    (set) => ({
      readRepliesAloud: true,
      speakNextReply: false,
      dictating: false,
      setReadRepliesAloud: (readRepliesAloud) => set({ readRepliesAloud }),
      setSpeakNextReply: (speakNextReply) => set({ speakNextReply }),
      markDictated: () => set({ dictating: true }),
      armSpeakOnSend: () =>
        set((s) =>
          s.dictating ? { dictating: false, speakNextReply: s.readRepliesAloud } : s,
        ),
      resetSpeech: () => set({ speakNextReply: false, dictating: false }),
    }),
    {
      name: "multica_voice_preferences",
      storage: createJSONStorage(() => defaultStorage),
      // Only the preference is durable; the two bridge flags die with the page.
      partialize: (state) => ({ readRepliesAloud: state.readRepliesAloud }),
    },
  ),
);
