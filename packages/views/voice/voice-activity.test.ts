// @vitest-environment node
/**
 * Smoke coverage for the views re-export. Full matrix lives in
 * `packages/core/voice/voice-activity.test.ts`.
 */
import { describe, expect, it, vi } from "vitest";
import { createVoiceActivityDetector, DEFAULT_VOICE_ACTIVITY } from "./voice-activity";

describe("createVoiceActivityDetector (views re-export)", () => {
  it("exports the shared detector used by web conversation mode", () => {
    const onSpeechEnd = vi.fn();
    const detector = createVoiceActivityDetector(undefined, { onSpeechEnd });
    detector.push(0.1, DEFAULT_VOICE_ACTIVITY.speechOnMs);
    detector.push(0.001, DEFAULT_VOICE_ACTIVITY.speechOffMs);
    expect(onSpeechEnd).toHaveBeenCalledTimes(1);
  });
});
