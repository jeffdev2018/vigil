/**
 * Duplex conversation mode on mobile (JEF-342 / N20): microphone stays open,
 * voice-activity detection cuts each turn at silence, the segment is
 * transcribed via `api.transcribeVoice`, and handed to `onUtterance` (the
 * chat composer sends it). Mirrors `packages/views/voice/use-voice-conversation.ts`
 * with expo-audio instead of MediaRecorder + Web Audio.
 *
 * TTS barge-in is a no-op here: mobile does not yet read replies aloud.
 */
import { useCallback, useEffect, useRef, useState } from "react";
import {
  RecordingPresets,
  requestRecordingPermissionsAsync,
  setAudioModeAsync,
  useAudioRecorder,
  useAudioRecorderState,
} from "expo-audio";
import { createVoiceActivityDetector } from "@multica/core/voice";
import { api, ApiError } from "@/data/api";
import { meteringToRms } from "@/lib/metering-to-rms";

export type VoiceConversationPhase = "idle" | "listening" | "transcribing";

export type VoiceConversationError =
  | "unsupported"
  | "mic_denied"
  | "not_configured"
  | "failed";

const SAMPLE_INTERVAL_MS = 60;
/** Hard cap on one segment: a turn this long is a monologue, cut it anyway. */
const MAX_SEGMENT_MS = 60_000;

const RECORDING_OPTIONS = {
  ...RecordingPresets.HIGH_QUALITY,
  isMeteringEnabled: true,
};

function sttErrorCode(err: unknown): string | undefined {
  if (err instanceof ApiError && err.body && typeof err.body === "object") {
    const code = (err.body as { code?: unknown }).code;
    if (typeof code === "string" && code.length > 0) return code;
  }
  return undefined;
}

export function useVoiceConversation(options: {
  onUtterance: (text: string) => void;
  onError: (error: VoiceConversationError) => void;
}) {
  const { onUtterance, onError } = options;
  const [phase, setPhase] = useState<VoiceConversationPhase>("idle");

  const recorder = useAudioRecorder(RECORDING_OPTIONS);
  const recorderState = useAudioRecorderState(recorder, SAMPLE_INTERVAL_MS);

  const activeRef = useRef(false);
  const cuttingRef = useRef(false);
  const segmentStartRef = useRef(0);
  const lastSampleRef = useRef(0);
  const detectorRef = useRef<ReturnType<typeof createVoiceActivityDetector> | null>(
    null,
  );
  const onUtteranceRef = useRef(onUtterance);
  const onErrorRef = useRef(onError);
  onUtteranceRef.current = onUtterance;
  onErrorRef.current = onError;

  const armNextSegment = useCallback(async () => {
    if (!activeRef.current) return;
    await recorder.prepareToRecordAsync();
    recorder.record();
    segmentStartRef.current = Date.now();
  }, [recorder]);

  const cutSegment = useCallback(
    async (opts: { upload: boolean }) => {
      if (!activeRef.current || cuttingRef.current) return;
      if (!recorder.isRecording) {
        if (activeRef.current) void armNextSegment();
        return;
      }
      cuttingRef.current = true;
      try {
        await recorder.stop();
        const uri = recorder.uri;
        if (activeRef.current) {
          try {
            await armNextSegment();
          } catch {
            onErrorRef.current("failed");
            activeRef.current = false;
            setPhase("idle");
            return;
          }
        }
        if (!opts.upload || !uri) return;
        setPhase("transcribing");
        try {
          const { text } = await api.transcribeVoice({
            uri,
            name: "memo.m4a",
            type: "audio/mp4",
          });
          if (activeRef.current && text.trim()) {
            onUtteranceRef.current(text.trim());
          }
        } catch (err) {
          const code = sttErrorCode(err);
          if (code === "stt_not_configured") {
            onErrorRef.current("not_configured");
            activeRef.current = false;
            try {
              if (recorder.isRecording) await recorder.stop();
            } catch {
              // ignore
            }
            setPhase("idle");
            return;
          }
          onErrorRef.current("failed");
        } finally {
          if (activeRef.current) setPhase("listening");
        }
      } finally {
        cuttingRef.current = false;
      }
    },
    [armNextSegment, recorder],
  );

  const start = useCallback(async () => {
    if (activeRef.current) return;
    let permission;
    try {
      permission = await requestRecordingPermissionsAsync();
    } catch {
      onErrorRef.current("unsupported");
      return;
    }
    if (!permission.granted) {
      onErrorRef.current("mic_denied");
      return;
    }
    try {
      await setAudioModeAsync({
        allowsRecording: true,
        playsInSilentMode: true,
      });
      detectorRef.current = createVoiceActivityDetector(undefined, {
        onSpeechStart: () => {
          // Barge-in placeholder: mobile has no reply TTS yet.
        },
        onSpeechEnd: () => {
          void cutSegment({ upload: true });
        },
        onIdleRestart: () => {
          void cutSegment({ upload: false });
        },
      });
      activeRef.current = true;
      lastSampleRef.current = Date.now();
      await armNextSegment();
      setPhase("listening");
    } catch {
      activeRef.current = false;
      detectorRef.current = null;
      onErrorRef.current("failed");
      setPhase("idle");
    }
  }, [armNextSegment, cutSegment]);

  const stop = useCallback(() => {
    if (!activeRef.current) return;
    activeRef.current = false;
    detectorRef.current = null;
    void (async () => {
      try {
        if (recorder.isRecording) await recorder.stop();
      } catch {
        // ignore
      }
      setPhase("idle");
    })();
  }, [recorder]);

  // Feed VAD from metering while listening.
  useEffect(() => {
    if (!activeRef.current || phase === "idle" || cuttingRef.current) return;
    const detector = detectorRef.current;
    if (!detector) return;
    const now = Date.now();
    const elapsed = Math.min(now - lastSampleRef.current, SAMPLE_INTERVAL_MS * 2);
    lastSampleRef.current = now;
    const rms = meteringToRms(recorderState.metering);
    detector.push(rms, elapsed);
    if (now - segmentStartRef.current > MAX_SEGMENT_MS) {
      void cutSegment({ upload: true });
    }
  }, [recorderState.metering, recorderState.durationMillis, phase, cutSegment]);

  useEffect(() => stop, [stop]);

  return { phase, start, stop };
}
