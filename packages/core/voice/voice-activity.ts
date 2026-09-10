/**
 * Voice-activity detection, as a pure state machine over RMS levels. The hook
 * that owns the microphone feeds it one level per sample tick with the time
 * between ticks; it says when a turn of speech starts (after sustained
 * above-threshold audio) and ends (after a silence gap), and when the recorder
 * has been idle long enough that its buffer should be dropped and restarted
 * rather than grow forever on silence.
 *
 * Pure on purpose: thresholds are numbers anyone can test; no Web Audio /
 * native recorder here. Shared by web (`packages/views/voice`) and mobile.
 */

export interface VoiceActivityOptions {
  /** RMS floor that counts as speech. 0.015–0.03 works for laptop mics. */
  speechThreshold: number;
  /** Sustained above-threshold audio (ms) before a turn is considered real. */
  speechOnMs: number;
  /** Silence (ms) after speech that ends the turn. */
  speechOffMs: number;
  /** Total silence (ms) after which the recorder should restart empty. */
  idleRestartMs: number;
}

export const DEFAULT_VOICE_ACTIVITY: VoiceActivityOptions = {
  speechThreshold: 0.02,
  speechOnMs: 140,
  speechOffMs: 700,
  idleRestartMs: 5000,
};

export interface VoiceActivityEvents {
  /** Speech was just confirmed; a turn is in progress. */
  onSpeechStart?: () => void;
  /** The turn ended: this is where the segment is cut and transcribed. */
  onSpeechEnd?: () => void;
  /** No speech for idleRestartMs: drop the recorder's buffer and restart. */
  onIdleRestart?: () => void;
}

export interface VoiceActivityDetector {
  /** Feed one RMS sample. `elapsedMs` is the time since the previous feed. */
  push(rms: number, elapsedMs: number): void;
}

export function createVoiceActivityDetector(
  options: Partial<VoiceActivityOptions> = {},
  events: VoiceActivityEvents = {},
): VoiceActivityDetector {
  const opts = { ...DEFAULT_VOICE_ACTIVITY, ...options };
  let speaking = false;
  let aboveMs = 0;
  let belowMs = 0;
  let idleMs = 0;
  return {
    push(rms, elapsedMs) {
      if (rms >= opts.speechThreshold) {
        aboveMs += elapsedMs;
        belowMs = 0;
        idleMs = 0;
        if (!speaking && aboveMs >= opts.speechOnMs) {
          speaking = true;
          events.onSpeechStart?.();
        }
        return;
      }
      aboveMs = 0;
      belowMs += elapsedMs;
      if (speaking) {
        if (belowMs >= opts.speechOffMs) {
          speaking = false;
          belowMs = 0;
          events.onSpeechEnd?.();
        }
        return;
      }
      // Quiet, no turn in progress: count toward the idle-restart guard.
      idleMs += elapsedMs;
      if (idleMs >= opts.idleRestartMs) {
        idleMs = 0;
        events.onIdleRestart?.();
      }
    },
  };
}
