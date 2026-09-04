// @vitest-environment node
import { describe, expect, it } from "vitest";
import { bytesToBase64, floatTo16BitPCM, parseRealtimeEvent } from "./realtime-transcriber";

describe("floatTo16BitPCM", () => {
  it("scales and clamps to signed 16-bit little-endian", () => {
    const bytes = floatTo16BitPCM(new Float32Array([0, 1, -1, 2, -2, 0.5]));
    const view = new DataView(bytes.buffer);
    expect(view.getInt16(0, true)).toBe(0);
    expect(view.getInt16(2, true)).toBe(0x7fff);
    expect(view.getInt16(4, true)).toBe(-0x8000);
    expect(view.getInt16(6, true)).toBe(0x7fff);
    expect(view.getInt16(8, true)).toBe(-0x8000);
    expect(view.getInt16(10, true)).toBe(Math.trunc(0.5 * 0x7fff));
  });
});

describe("bytesToBase64", () => {
  it("encodes buffers larger than one call frame", () => {
    const bytes = new Uint8Array(70_000).map((_, i) => i % 251);
    expect(bytesToBase64(bytes)).toBe(Buffer.from(bytes).toString("base64"));
  });
});

describe("parseRealtimeEvent", () => {
  it("maps the provider events we act on and ignores the rest", () => {
    expect(parseRealtimeEvent(JSON.stringify({ type: "transcription.text.delta", text: "Bon" }))).toEqual({
      kind: "delta",
      text: "Bon",
    });
    expect(parseRealtimeEvent(JSON.stringify({ type: "transcription.done" }))).toEqual({ kind: "done" });
    expect(parseRealtimeEvent(JSON.stringify({ type: "error", error: { message: "bad" } }))).toEqual({
      kind: "error",
      message: "bad",
    });
    expect(parseRealtimeEvent(JSON.stringify({ type: "session.created", session: {} }))).toBeNull();
    expect(parseRealtimeEvent("not json")).toBeNull();
    expect(parseRealtimeEvent(new ArrayBuffer(2))).toBeNull();
  });
});
