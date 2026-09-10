// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { createVoiceActivityDetector, DEFAULT_VOICE_ACTIVITY } from "./voice-activity";

const SILENCE = 0.001;
const SPEECH = 0.1;

/** Feed levels at fixed ticks; returns the detector. */
function feed(
  levels: Array<[number, number]>,
  events: { onSpeechStart?: () => void; onSpeechEnd?: () => void; onIdleRestart?: () => void },
) {
  const detector = createVoiceActivityDetector(undefined, events);
  for (const [level, ms] of levels) detector.push(level, ms);
  return detector;
}

describe("createVoiceActivityDetector", () => {
  it("starts a turn only after sustained speech, not on a click or a pop", () => {
    const onSpeechStart = vi.fn();
    // A single loud tick (30ms) below speechOnMs must not start a turn.
    feed(
      [
        [SPEECH, 30],
        [SILENCE, 200],
      ],
      { onSpeechStart },
    );
    expect(onSpeechStart).not.toHaveBeenCalled();
  });

  it("starts a turn after the on-threshold, ends it after the silence gap", () => {
    const onSpeechStart = vi.fn();
    const onSpeechEnd = vi.fn();
    feed(
      [
        [SPEECH, 100],
        [SPEECH, 100], // 200ms >= speechOnMs: turn starts
        [SILENCE, 300], // below speechOffMs: turn continues
        [SILENCE, 500], // 800ms total silence: turn ends
      ],
      { onSpeechStart, onSpeechEnd },
    );
    expect(onSpeechStart).toHaveBeenCalledTimes(1);
    expect(onSpeechEnd).toHaveBeenCalledTimes(1);
  });

  it("keeps the turn alive through a mid-sentence pause shorter than the gap", () => {
    const onSpeechStart = vi.fn();
    const onSpeechEnd = vi.fn();
    feed(
      [
        [SPEECH, 200],
        [SILENCE, 400], // pause...
        [SPEECH, 100], // ...resumed before speechOffMs elapsed
        [SILENCE, 800],
      ],
      { onSpeechStart, onSpeechEnd },
    );
    expect(onSpeechStart).toHaveBeenCalledTimes(1);
    expect(onSpeechEnd).toHaveBeenCalledTimes(1);
  });

  it("restarts the idle recorder only outside a turn, once per idle window", () => {
    const onIdleRestart = vi.fn();
    const idleWindow = DEFAULT_VOICE_ACTIVITY.idleRestartMs;
    feed(
      [
        [SILENCE, idleWindow],
        [SILENCE, idleWindow - 10], // cumulative within one window: still zero events
        [SILENCE, 20], // window reached now
        [SPEECH, DEFAULT_VOICE_ACTIVITY.speechOnMs + 50],
        [SILENCE, idleWindow + 100], // silence DURING a turn never restarts the recorder
      ],
      { onIdleRestart },
    );
    expect(onIdleRestart).toHaveBeenCalledTimes(2);
  });

  it("a loud tick during silence resets the idle accumulator (no premature restart)", () => {
    const onIdleRestart = vi.fn();
    const window = DEFAULT_VOICE_ACTIVITY.idleRestartMs;
    feed(
      [
        [SILENCE, window - 100],
        [SPEECH, 30], // noise, not a turn
        [SILENCE, window - 100], // window NOT reached: the noise reset the clock
      ],
      { onIdleRestart },
    );
    expect(onIdleRestart).not.toHaveBeenCalled();
  });
});
