"use client";

import { useEffect, useState } from "react";
import { speechTextFromMarkdown, splitUtterances } from "./speech-text";

/**
 * Text-to-speech through the browser's own SpeechSynthesis: no server, no
 * key, works offline with the OS voices (web, Electron). One thing speaks at a
 * time; starting a new text stops the previous one.
 *
 * Listeners subscribe to a tiny module-level state so a button can show
 * "stop" while its message is being read, wherever it is on the page.
 */

type Listener = () => void;
const listeners = new Set<Listener>();
let speakingId: string | null = null;

function notify() {
  for (const l of listeners) l();
}

export function isSpeechSupported(): boolean {
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
  if (!isSpeechSupported()) return;
  window.speechSynthesis.cancel();
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
  window.speechSynthesis.cancel();
  const text = speechTextFromMarkdown(markdown);
  const chunks = splitUtterances(text);
  if (chunks.length === 0) return;
  speakingId = id;
  notify();
  const voice = voiceFor(lang);
  const finish = () => {
    if (speakingId === id) {
      speakingId = null;
      notify();
    }
  };
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
