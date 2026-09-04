import { create } from "zustand";

/**
 * Client state for voice interaction. Not persisted: it only bridges one
 * dictated message to the reply that answers it.
 *
 * `speakNextReply` is set when the user dictates a chat message and consumed
 * by the first assistant reply that lands afterwards, which is then read
 * aloud. Anything else about speech (what is currently being spoken) lives
 * in the browser's SpeechSynthesis, not here.
 */
interface VoiceState {
  speakNextReply: boolean;
  setSpeakNextReply: (value: boolean) => void;
}

export const useVoiceStore = create<VoiceState>((set) => ({
  speakNextReply: false,
  setSpeakNextReply: (speakNextReply) => set({ speakNextReply }),
}));
