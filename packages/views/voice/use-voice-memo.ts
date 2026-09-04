"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { errorCode, getApi } from "@multica/core/api";

export type VoiceMemoPhase = "idle" | "recording" | "transcribing";

export type VoiceMemoError =
  | "unsupported"
  | "mic_denied"
  | "not_configured"
  | "failed";

const PREFERRED_MIME = "audio/webm;codecs=opus";

/**
 * Push-to-talk memo: record the microphone until `stop()`, send the audio to
 * the server's transcription endpoint, hand the text back. Browser-only; the
 * MediaRecorder and the tracks live in refs so an unmount always releases
 * the microphone.
 */
export function useVoiceMemo(options: {
  onText: (text: string) => void;
  onError: (error: VoiceMemoError) => void;
}) {
  const { onText, onError } = options;
  const [phase, setPhase] = useState<VoiceMemoPhase>("idle");
  const recorderRef = useRef<MediaRecorder | null>(null);
  const streamRef = useRef<MediaStream | null>(null);
  const chunksRef = useRef<Blob[]>([]);
  const cancelledRef = useRef(false);
  const onTextRef = useRef(onText);
  const onErrorRef = useRef(onError);
  onTextRef.current = onText;
  onErrorRef.current = onError;

  const release = useCallback(() => {
    for (const track of streamRef.current?.getTracks() ?? []) track.stop();
    streamRef.current = null;
    recorderRef.current = null;
  }, []);

  const start = useCallback(async () => {
    if (recorderRef.current) return;
    if (
      typeof MediaRecorder === "undefined" ||
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
    chunksRef.current = [];
    cancelledRef.current = false;
    const recorder = MediaRecorder.isTypeSupported(PREFERRED_MIME)
      ? new MediaRecorder(stream, { mimeType: PREFERRED_MIME })
      : new MediaRecorder(stream);
    recorder.ondataavailable = (event: BlobEvent) => {
      if (event.data.size > 0) chunksRef.current.push(event.data);
    };
    recorder.onstop = async () => {
      const blob = new Blob(chunksRef.current, { type: recorder.mimeType || "audio/webm" });
      release();
      if (cancelledRef.current || blob.size === 0) {
        setPhase("idle");
        return;
      }
      setPhase("transcribing");
      try {
        const { text } = await getApi().transcribeVoice(blob);
        if (text.trim()) onTextRef.current(text.trim());
      } catch (err) {
        const code = errorCode(err);
        onErrorRef.current(code === "stt_not_configured" ? "not_configured" : "failed");
      } finally {
        setPhase("idle");
      }
    };
    recorderRef.current = recorder;
    recorder.start();
    setPhase("recording");
  }, [release]);

  const stop = useCallback(() => {
    const recorder = recorderRef.current;
    if (!recorder) return;
    if (recorder.state !== "inactive") recorder.stop();
    else release();
  }, [release]);

  const cancel = useCallback(() => {
    cancelledRef.current = true;
    stop();
  }, [stop]);

  // Unmounting mid-recording must not keep the microphone indicator on.
  useEffect(() => {
    return () => {
      cancelledRef.current = true;
      const recorder = recorderRef.current;
      if (recorder && recorder.state !== "inactive") recorder.stop();
      release();
    };
  }, [release]);

  return { phase, start, stop, cancel };
}
