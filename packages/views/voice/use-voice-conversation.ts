"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { errorCode, getApi } from "@multica/core/api";
import { useVoiceStore, voiceTranscriptionLanguage } from "@multica/core/voice";
import { createVoiceActivityDetector } from "./voice-activity";
import { stopSpeaking } from "./speech";

export type VoiceConversationPhase = "idle" | "listening" | "transcribing";

export type VoiceConversationError =
  | "unsupported"
  | "mic_denied"
  | "not_configured"
  | "failed";

const PREFERRED_MIME = "audio/webm;codecs=opus";
const SAMPLE_INTERVAL_MS = 60;
/** Hard cap on one segment: a turn this long is a monologue, cut it anyway. */
const MAX_SEGMENT_MS = 60_000;

/**
 * Duplex conversation mode (JEF-316 follow-up): the microphone stays open,
 * voice-activity detection cuts each turn at its silence, the segment is
 * transcribed and handed to `onUtterance` (the composer sends it), and any
 * reply being read aloud is stopped the moment the user talks over it —
 * barge-in is what makes it a conversation instead of two dictaphones.
 *
 * While the agent answers, nothing here records a turn to send: the reply is
 * the server's own audio only when it arrives; this side just keeps the mic
 * open for the user's next turn (or their interruption).
 */
export function useVoiceConversation(options: {
  onUtterance: (text: string) => void;
  onError: (error: VoiceConversationError) => void;
}) {
  const { onUtterance, onError } = options;
  const [phase, setPhase] = useState<VoiceConversationPhase>("idle");

  const streamRef = useRef<MediaStream | null>(null);
  const audioCtxRef = useRef<AudioContext | null>(null);
  const timerRef = useRef<number | null>(null);
  const recorderRef = useRef<MediaRecorder | null>(null);
  const chunksRef = useRef<Blob[]>([]);
  const segmentStartRef = useRef(0);
  const activeRef = useRef(false);
  const lastSampleRef = useRef(0);
  const onUtteranceRef = useRef(onUtterance);
  const onErrorRef = useRef(onError);
  onUtteranceRef.current = onUtterance;
  onErrorRef.current = onError;

  const releaseCapture = useCallback(() => {
    if (timerRef.current !== null) {
      window.clearInterval(timerRef.current);
      timerRef.current = null;
    }
    const recorder = recorderRef.current;
    recorderRef.current = null;
    if (recorder && recorder.state !== "inactive") recorder.stop();
    void audioCtxRef.current?.close().catch(() => {});
    audioCtxRef.current = null;
    for (const track of streamRef.current?.getTracks() ?? []) track.stop();
    streamRef.current = null;
  }, []);

  const startSegment = useCallback(() => {
    const stream = streamRef.current;
    if (!stream || recorderRef.current || !activeRef.current) return;
    chunksRef.current = [];
    segmentStartRef.current = Date.now();
    const recorder = MediaRecorder.isTypeSupported(PREFERRED_MIME)
      ? new MediaRecorder(stream, { mimeType: PREFERRED_MIME })
      : new MediaRecorder(stream);
    recorder.ondataavailable = (event: BlobEvent) => {
      if (event.data.size > 0 && recorderRef.current === recorder) {
        chunksRef.current.push(event.data);
      }
    };
    recorder.onstop = async () => {
      const blob = new Blob(chunksRef.current, {
        type: recorder.mimeType || "audio/webm",
      });
      // Arm the next segment before the (possibly slow) transcription so the
      // mic keeps listening; the next turn is not held hostage to the API.
      if (activeRef.current) startSegment();
      if (blob.size === 0) return;
      setPhase("transcribing");
      try {
        const { text } = await getApi().transcribeVoice(
          blob,
          voiceTranscriptionLanguage(useVoiceStore.getState().voiceLanguage),
        );
        if (activeRef.current && text.trim()) onUtteranceRef.current(text.trim());
      } catch (err) {
        const code = errorCode(err);
        // Transcription failures do not end the conversation: the user can
        // repeat the turn. Configuration problems are permanent, so they do.
        if (code === "stt_not_configured") {
          onErrorRef.current("not_configured");
          activeRef.current = false;
          releaseCapture();
          setPhase("idle");
          return;
        }
        onErrorRef.current("failed");
      } finally {
        if (activeRef.current) setPhase("listening");
      }
    };
    recorderRef.current = recorder;
    recorder.start();
  }, [releaseCapture]);

  const start = useCallback(async () => {
    if (activeRef.current) return;
    if (
      typeof MediaRecorder === "undefined" ||
      typeof AudioContext === "undefined" ||
      typeof navigator === "undefined" ||
      !navigator.mediaDevices?.getUserMedia
    ) {
      onErrorRef.current("unsupported");
      return;
    }
    let stream: MediaStream;
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    } catch {
      onErrorRef.current("mic_denied");
      return;
    }
    streamRef.current = stream;
    const audioCtx = new AudioContext();
    audioCtxRef.current = audioCtx;
    const source = audioCtx.createMediaStreamSource(stream);
    const analyser = audioCtx.createAnalyser();
    analyser.fftSize = 512;
    source.connect(analyser);
    const buffer = new Uint8Array(analyser.fftSize);

    const detector = createVoiceActivityDetector(undefined, {
      onSpeechStart: () => {
        // Barge-in: the user talking over a reply being read stops the read;
        // whatever they say next is captured as the new turn.
        stopSpeaking();
      },
      onSpeechEnd: () => {
        const recorder = recorderRef.current;
        if (recorder && recorder.state !== "inactive") recorder.stop();
        else startSegment();
      },
      onIdleRestart: () => {
        // Silence has been accumulating in the recorder for nothing: drop the
        // buffer and start a fresh segment so memory stays flat.
        const recorder = recorderRef.current;
        if (recorder && recorder.state !== "inactive") {
          recorderRef.current = null;
          recorder.onstop = null;
          recorder.stop();
          startSegment();
        }
      },
    });

    activeRef.current = true;
    lastSampleRef.current = Date.now();
    startSegment();
    setPhase("listening");
    timerRef.current = window.setInterval(() => {
      if (!activeRef.current) return;
      analyser.getByteTimeDomainData(buffer);
      let sumSquares = 0;
      for (let i = 0; i < buffer.length; i++) {
        const v = (buffer[i]! - 128) / 128;
        sumSquares += v * v;
      }
      const rms = Math.sqrt(sumSquares / buffer.length);
      const now = Date.now();
      const elapsed = Math.min(now - lastSampleRef.current, SAMPLE_INTERVAL_MS * 2);
      lastSampleRef.current = now;
      detector.push(rms, elapsed);
      if (now - segmentStartRef.current > MAX_SEGMENT_MS) {
        // A turn that never paused: cut at the cap; the transcriber takes
        // whole segments and a monologue must not become an unbounded blob.
        const recorder = recorderRef.current;
        if (recorder && recorder.state !== "inactive") recorder.stop();
      }
    }, SAMPLE_INTERVAL_MS);
  }, [startSegment]);

  const stop = useCallback(() => {
    if (!activeRef.current) return;
    activeRef.current = false;
    stopSpeaking();
    releaseCapture();
    setPhase("idle");
  }, [releaseCapture]);

  // Unmounting mid-conversation must not keep the microphone indicator on.
  useEffect(() => stop, [stop]);

  return { phase, start, stop };
}
