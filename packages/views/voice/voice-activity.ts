/**
 * Re-export the pure VAD from `@multica/core/voice` so web/desktop keep a
 * stable import path under `packages/views/voice/`. The algorithm lives in
 * core so mobile can share it without importing `@multica/views`.
 */
export {
  createVoiceActivityDetector,
  DEFAULT_VOICE_ACTIVITY,
  type VoiceActivityDetector,
  type VoiceActivityEvents,
  type VoiceActivityOptions,
} from "@multica/core/voice";
