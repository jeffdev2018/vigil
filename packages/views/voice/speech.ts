"use client";

import { useEffect, useState } from "react";
import { speechTextFromMarkdown, splitUtterances } from "./speech-text";
import {
  SERVER_SPEECH_MAX_CHARS,
  isServerSpeechAvailable,
  playServerSpeech,
  type ServerSpeechHandle,
} from "./server-speech";

/**
 * Text-to-speech, in two voices. When the deployment configures a provider
 * (MULTICA_TTS_*, declared as `tts_available`), the server synthesizes the
 * audio and this plays it; otherwise — and whenever a request fails — the
 * browser's own SpeechSynthesis reads it, which needs no server, no key, and
 * works offline with the OS voices (web, Electron).
 *
 * One thing speaks at a time; starting a new text stops the previous one.
 *
 * Listeners subscribe to a tiny module-level state so a button can show
 * "stop" while its message is being read, wherever it is on the page.
 */

type Listener = () => void;
const listeners = new Set<Listener>();
let speakingId: string | null = null;
let serverPlayback: ServerSpeechHandle | null = null;

function notify() {
  for (const l of listeners) l();
}

export function isSpeechSupported(): boolean {
  if (typeof window === "undefined") return false;
  if (isServerSpeechAvailable() && typeof Audio !== "undefined") return true;
  return (
    "speechSynthesis" in window && typeof SpeechSynthesisUtterance !== "undefined"
  );
}

function isBrowserSpeechSupported(): boolean {
  return (
    typeof window !== "undefined" &&
    "speechSynthesis" in window &&
    typeof SpeechSynthesisUtterance !== "undefined"
  );
}

/** Pick a voice for the locale when one exists; the default voice otherwise. */
function voiceFor(lang: string): SpeechSynthesisVoice | null {
  const voices = window.speechSynthesis.getVoices();
  const base = lang.toLowerCase().split("-")[0] ?? lang.toLowerCase();
  return (
    voices.find((v) => v.lang.toLowerCase() === lang.toLowerCase()) ??
    voices.find((v) => v.lang.toLowerCase().startsWith(base)) ??
    null
  );
}

export function stopSpeaking(): void {
  if (typeof window === "undefined") return;
  serverPlayback?.stop();
  serverPlayback = null;
  if (isBrowserSpeechSupported()) window.speechSynthesis.cancel();
  if (speakingId !== null) {
    speakingId = null;
    notify();
  }
}

/**
 * Read `markdown` aloud, identified by `id` (a message id). Calling again
 * with the same id while it speaks stops it.
 */
export function speakMarkdown(id: string, markdown: string, lang: string): void {
  if (!isSpeechSupported()) return;
  if (speakingId === id) {
    stopSpeaking();
    return;
  }
  stopSpeaking();
  const text = speechTextFromMarkdown(markdown);
  if (!text) return;
  speakingId = id;
  notify();
  const finish = () => {
    if (speakingId === id) {
      serverPlayback = null;
      speakingId = null;
      notify();
    }
  };
  if (isServerSpeechAvailable() && typeof Audio !== "undefined") {
    // Sentence groups rather than the whole text: the endpoint refuses
    // anything past its cap, and long text would also mean a long silence
    // before the first word.
    const blocks = splitUtterances(text, SERVER_SPEECH_MAX_CHARS);
    if (blocks.length === 0) return finish();
    serverPlayback = playServerSpeech(blocks, lang, finish, () => {
      // The provider (or the network) failed mid-read: fall back to the
      // browser voice rather than going silent.
      serverPlayback = null;
      if (speakingId !== id) return;
      speakBrowser(text, lang, finish);
    });
    return;
  }
  speakBrowser(text, lang, finish);
}

/** The browser voice: default, and fallback when the server voice fails. */
function speakBrowser(text: string, lang: string, finish: () => void): void {
  if (!isBrowserSpeechSupported()) {
    finish();
    return;
  }
  window.speechSynthesis.cancel();
  const chunks = splitUtterances(text);
  if (chunks.length === 0) {
    finish();
    return;
  }
  const voice = voiceFor(lang);
  chunks.forEach((chunk, i) => {
    const u = new SpeechSynthesisUtterance(chunk);
    u.lang = lang;
    if (voice) u.voice = voice;
    if (i === chunks.length - 1) {
      u.onend = finish;
    }
    u.onerror = finish;
    window.speechSynthesis.speak(u);
  });
}

/** Whether the message with `id` is currently being read aloud. */
export function useIsSpeaking(id: string): boolean {
  const [speaking, setSpeaking] = useState(() => speakingId === id);
  useEffect(() => {
    const update = () => setSpeaking(speakingId === id);
    listeners.add(update);
    update();
    return () => {
      listeners.delete(update);
    };
  }, [id]);
  return speaking;
}
