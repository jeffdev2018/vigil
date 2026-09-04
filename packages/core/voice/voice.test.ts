// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { useVoiceStore } from "./store";

function stubFetchJson(body: unknown, status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("transcribeVoice", () => {
  it("posts the audio as multipart and returns the text", async () => {
    stubFetchJson({ text: "Bonjour" });
    const api = new ApiClient("https://api.example.test");
    const res = await api.transcribeVoice(new Blob(["x"], { type: "audio/webm" }));
    expect(res.text).toBe("Bonjour");
    const call = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[0] as [string, RequestInit];
    expect(String(call[0])).toContain("/api/voice/transcribe");
    expect(call[1].body).toBeInstanceOf(FormData);
  });

  it("degrades a malformed body to an empty transcription instead of throwing", async () => {
    stubFetchJson({ text: 42 });
    const api = new ApiClient("https://api.example.test");
    await expect(api.transcribeVoice(new Blob(["x"]))).resolves.toEqual({ text: "" });
  });
});

describe("useVoiceStore", () => {
  it("remembers that the next reply should be spoken until consumed", () => {
    expect(useVoiceStore.getState().speakNextReply).toBe(false);
    useVoiceStore.getState().setSpeakNextReply(true);
    expect(useVoiceStore.getState().speakNextReply).toBe(true);
    useVoiceStore.getState().setSpeakNextReply(false);
    expect(useVoiceStore.getState().speakNextReply).toBe(false);
  });
});
