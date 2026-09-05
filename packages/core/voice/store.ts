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
 * `readRepliesAloud` and `voiceLanguage` are the durable things here —
 * Settings preferences, persisted; the two flags above are ephemeral and
 * never written.
 */

/**
 * Languages the voice features can be pinned to. "auto" follows the app
 * locale, which is right until someone runs the app in English and dictates
 * in French — the transcriber then guesses, badly, on every memo.
 *
 * Deliberately the five the product ships UI for, not an open string: the
 * value is sent to the transcription endpoint, which validates it against the
 * same list.
 */
export type VoiceLanguage = "auto" | "en" | "fr" | "ja" | "ko" | "zh";

export const VOICE_LANGUAGES: VoiceLanguage[] = [
  "auto",
  "en",
  "fr",
  "ja",
  "ko",
  "zh",
];

interface VoiceState {
  /**
   * Settings → Chat. When off, a dictated message behaves like a typed one and
   * nothing speaks by itself; the per-message Listen button still works.
   */
  readRepliesAloud: boolean;
  setReadRepliesAloud: (value: boolean) => void;
  /**
   * Settings → Chat. The language dictation is transcribed in, and the voice
   * replies are spoken in. "auto" follows the app locale.
   */
  voiceLanguage: VoiceLanguage;
  setVoiceLanguage: (value: VoiceLanguage) => void;
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
      voiceLanguage: "auto",
      speakNextReply: false,
      dictating: false,
      setReadRepliesAloud: (readRepliesAloud) => set({ readRepliesAloud }),
      setVoiceLanguage: (voiceLanguage) => set({ voiceLanguage }),
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
      // Only the preferences are durable; the two bridge flags die with the page.
      partialize: (state) => ({
        readRepliesAloud: state.readRepliesAloud,
        voiceLanguage: state.voiceLanguage,
      }),
    },
  ),
);

/**
 * BCP 47 tag for SpeechSynthesis and for the transcriber, from the voice
 * preference and the app locale. `null` means "let the provider decide" — only
 * reachable from "auto" with a locale the product has no tag for, where
 * guessing would be worse than not saying.
 */
export function resolveVoiceLocale(
  preference: VoiceLanguage,
  appLocale: string,
): string {
  const language = preference === "auto" ? appLocale : preference;
  switch (language) {
    case "fr":
      return "fr-FR";
    case "ja":
      return "ja-JP";
    case "ko":
      return "ko-KR";
    case "zh":
    case "zh-Hans":
      return "zh-CN";
    case "en":
      return "en-US";
    default:
      // A locale the product ships but this list predates: keep a regioned tag
      // as-is, and fall back to English rather than an unspeakable bare code.
      return language.includes("-") ? language : "en-US";
  }
}

/**
 * The `language` field the transcription endpoint takes: a bare ISO-639-1
 * code, or "" for "use the server default" (MULTICA_STT_LANGUAGE). "auto"
 * deliberately sends nothing rather than the app locale — a user reading the
 * app in English is not necessarily dictating in it, and the server default is
 * the deployment's own answer to that.
 */
export function voiceTranscriptionLanguage(preference: VoiceLanguage): string {
  return preference === "auto" ? "" : preference;
}
