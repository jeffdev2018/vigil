/**
 * Convert expo-audio / AVAudioRecorder metering (dB, typically −160…0) into
 * a 0…1 amplitude suitable for `createVoiceActivityDetector`. Same speech
 * threshold as web (~0.02) maps to roughly −34 dB.
 */
export function meteringToRms(metering: number | undefined | null): number {
  if (metering == null || !Number.isFinite(metering)) return 0;
  // Floor very quiet readings so idle silence doesn't float near the threshold.
  const clamped = Math.max(-60, Math.min(0, metering));
  return Math.pow(10, clamped / 20);
}
