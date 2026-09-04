// Browser-side client for the provider's realtime transcription WebSocket
// (Mistral Voxtral Realtime contract): the server minted a short-lived token,
// the page streams 16 kHz PCM and receives text deltas as they are spoken.
//
// Wire protocol (JSON text frames):
//   -> {type:"session.update", session:{audio_format:{encoding, sample_rate}}}
//   -> {type:"input_audio.append", audio:<base64 pcm_s16le>}
//   -> {type:"input_audio.end"}
//   <- {type:"session.created", session:{...}}
//   <- {type:"transcription.text.delta", text}
//   <- {type:"transcription.done"} | {type:"error", error:{message}}

import type { RealtimeVoiceSession } from "@multica/core/types";

/** Float32 samples in [-1, 1] -> little-endian signed 16-bit PCM bytes. */
export function floatTo16BitPCM(samples: Float32Array): Uint8Array {
  const out = new Uint8Array(samples.length * 2);
  const view = new DataView(out.buffer);
  for (let i = 0; i < samples.length; i++) {
    const s = Math.max(-1, Math.min(1, samples[i] ?? 0));
    view.setInt16(i * 2, s < 0 ? s * 0x8000 : s * 0x7fff, true);
  }
  return out;
}

/** Base64 without the "binary string" apply() size limit. */
export function bytesToBase64(bytes: Uint8Array): string {
  let binary = "";
  const chunk = 0x8000;
  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunk));
  }
  return btoa(binary);
}

/** One realtime event as we consume it; anything else is ignored. */
export function parseRealtimeEvent(
  data: unknown,
): { kind: "delta"; text: string } | { kind: "done" } | { kind: "error"; message: string } | null {
  if (typeof data !== "string") return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(data);
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== "object") return null;
  const ev = parsed as { type?: unknown; text?: unknown; error?: { message?: unknown } };
  switch (ev.type) {
    case "transcription.text.delta":
      return typeof ev.text === "string" ? { kind: "delta", text: ev.text } : null;
    case "transcription.done":
      return { kind: "done" };
    case "error":
      return {
        kind: "error",
        message: typeof ev.error?.message === "string" ? ev.error.message : "realtime error",
      };
    default:
      return null;
  }
}

export interface RealtimeTranscriber {
  /** Sends the end-of-audio marker, waits briefly for the final text, closes. */
  stop: () => Promise<void>;
}

const SEND_EVERY_SAMPLES = 3200; // 200 ms at 16 kHz
const OPEN_TIMEOUT_MS = 6_000;
const DONE_GRACE_MS = 4_000;

/**
 * Streams `stream` (any sample rate; the context resamples) to the realtime
 * endpoint. Resolves once the socket is open and the session is configured,
 * rejects if it cannot connect — the caller then falls back to batch upload.
 */
export function startRealtimeTranscriber(opts: {
  stream: MediaStream;
  session: RealtimeVoiceSession;
  onDelta: (text: string) => void;
  onError: (message: string) => void;
}): Promise<RealtimeTranscriber> {
  return new Promise((resolve, reject) => {
    const sampleRate = opts.session.sample_rate || 16000;
    const ws = new WebSocket(opts.session.url, ["realtime", opts.session.token]);
    let settled = false;
    let done: (() => void) | null = null;
    let ctx: AudioContext | null = null;
    let processor: ScriptProcessorNode | null = null;
    let source: MediaStreamAudioSourceNode | null = null;
    let pending: Float32Array[] = [];
    let pendingSamples = 0;

    const openTimer = setTimeout(() => {
      if (settled) return;
      settled = true;
      ws.close();
      reject(new Error("realtime: open timeout"));
    }, OPEN_TIMEOUT_MS);

    const flushAudio = () => {
      if (ws.readyState !== WebSocket.OPEN || pendingSamples === 0) return;
      const merged = new Float32Array(pendingSamples);
      let offset = 0;
      for (const part of pending) {
        merged.set(part, offset);
        offset += part.length;
      }
      pending = [];
      pendingSamples = 0;
      ws.send(
        JSON.stringify({ type: "input_audio.append", audio: bytesToBase64(floatTo16BitPCM(merged)) }),
      );
    };

    const teardownAudio = () => {
      processor?.disconnect();
      source?.disconnect();
      processor = null;
      source = null;
      const c = ctx;
      ctx = null;
      if (c && c.state !== "closed") void c.close().catch(() => {});
    };

    ws.onopen = () => {
      ws.send(
        JSON.stringify({
          type: "session.update",
          session: { audio_format: { encoding: opts.session.encoding || "pcm_s16le", sample_rate: sampleRate } },
        }),
      );
      // ScriptProcessorNode is deprecated but universally available and needs
      // no worklet file to ship; the buffer is small enough for sub-second
      // latency. ponytail: swap for an AudioWorklet if CPU shows up in profiles.
      ctx = new AudioContext({ sampleRate });
      source = ctx.createMediaStreamSource(opts.stream);
      processor = ctx.createScriptProcessor(2048, 1, 1);
      processor.onaudioprocess = (e) => {
        const input = e.inputBuffer.getChannelData(0);
        pending.push(new Float32Array(input));
        pendingSamples += input.length;
        if (pendingSamples >= SEND_EVERY_SAMPLES) flushAudio();
      };
      source.connect(processor);
      processor.connect(ctx.destination);
      clearTimeout(openTimer);
      settled = true;
      resolve({
        stop: () =>
          new Promise<void>((resolveStop) => {
            teardownAudio();
            flushAudio();
            if (ws.readyState !== WebSocket.OPEN) {
              resolveStop();
              return;
            }
            const timer = setTimeout(() => {
              done = null;
              ws.close();
              resolveStop();
            }, DONE_GRACE_MS);
            done = () => {
              clearTimeout(timer);
              done = null;
              ws.close();
              resolveStop();
            };
            ws.send(JSON.stringify({ type: "input_audio.end" }));
          }),
      });
    };
    ws.onmessage = (event) => {
      const ev = parseRealtimeEvent(event.data);
      if (!ev) return;
      if (ev.kind === "delta") opts.onDelta(ev.text);
      else if (ev.kind === "done") done?.();
      else if (ev.kind === "error") opts.onError(ev.message);
    };
    ws.onerror = () => {
      if (!settled) {
        settled = true;
        clearTimeout(openTimer);
        reject(new Error("realtime: connection failed"));
        return;
      }
      opts.onError("connection error");
    };
    ws.onclose = () => {
      teardownAudio();
      if (!settled) {
        settled = true;
        clearTimeout(openTimer);
        reject(new Error("realtime: closed before open"));
        return;
      }
      done?.();
    };
  });
}
