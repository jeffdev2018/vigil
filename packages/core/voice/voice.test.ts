// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import {
  resolveVoiceLocale,
  useVoiceStore,
  voiceTranscriptionLanguage,
} from "./store";

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

  it("sends the chosen language and omits it when there is none", async () => {
    for (const [language, expected] of [["fr", "fr"], ["", null]] as const) {
      stubFetchJson({ text: "ok" });
      const api = new ApiClient("https://api.example.test");
      await api.transcribeVoice(new Blob(["x"]), language);
      const call = (fetch as unknown as ReturnType<typeof vi.fn>).mock
        .calls[0] as [string, RequestInit];
      expect((call[1].body as FormData).get("language")).toBe(expected);
      vi.unstubAllGlobals();
    }
  });

  it("degrades a malformed body to an empty transcription instead of throwing", async () => {
    stubFetchJson({ text: 42 });
    const api = new ApiClient("https://api.example.test");
    await expect(api.transcribeVoice(new Blob(["x"]))).resolves.toEqual({ text: "" });
  });
});

describe("useVoiceStore", () => {
  beforeEach(() => {
    useVoiceStore.getState().resetSpeech();
    useVoiceStore.getState().setReadRepliesAloud(true);
    useVoiceStore.getState().setVoiceLanguage("auto");
  });

  it("defaults the voice language to auto and remembers a choice", () => {
    expect(useVoiceStore.getState().voiceLanguage).toBe("auto");
    useVoiceStore.getState().setVoiceLanguage("fr");
    expect(useVoiceStore.getState().voiceLanguage).toBe("fr");
  });

  it("remembers that the next reply should be spoken until consumed", () => {
    expect(useVoiceStore.getState().speakNextReply).toBe(false);
    useVoiceStore.getState().setSpeakNextReply(true);
    expect(useVoiceStore.getState().speakNextReply).toBe(true);
    useVoiceStore.getState().setSpeakNextReply(false);
    expect(useVoiceStore.getState().speakNextReply).toBe(false);
  });

  it("arms the reply on the send of a dictated message, not on the transcription", () => {
    useVoiceStore.getState().markDictated();
    // Transcribing alone must not arm anything: the memo is still a draft.
    expect(useVoiceStore.getState().speakNextReply).toBe(false);
    useVoiceStore.getState().armSpeakOnSend();
    expect(useVoiceStore.getState().speakNextReply).toBe(true);
    expect(useVoiceStore.getState().dictating).toBe(false);
  });

  it("leaves a typed message silent", () => {
    useVoiceStore.getState().armSpeakOnSend();
    expect(useVoiceStore.getState().speakNextReply).toBe(false);
  });

  it("does not carry a dictated turn into the next send", () => {
    useVoiceStore.getState().markDictated();
    useVoiceStore.getState().armSpeakOnSend();
    useVoiceStore.getState().setSpeakNextReply(false); // the reply spoke it
    useVoiceStore.getState().armSpeakOnSend();
    expect(useVoiceStore.getState().speakNextReply).toBe(false);
  });

  it("reads replies aloud by default and honours the preference being turned off", () => {
    expect(useVoiceStore.getState().readRepliesAloud).toBe(true);
    useVoiceStore.getState().setReadRepliesAloud(false);
    useVoiceStore.getState().markDictated();
    useVoiceStore.getState().armSpeakOnSend();
    // The turn is still consumed — it just does not speak.
    expect(useVoiceStore.getState().speakNextReply).toBe(false);
    expect(useVoiceStore.getState().dictating).toBe(false);
  });

  it("resetSpeech drops both the armed reply and an unsent memo", () => {
    useVoiceStore.getState().markDictated();
    useVoiceStore.getState().setSpeakNextReply(true);
    useVoiceStore.getState().resetSpeech();
    expect(useVoiceStore.getState()).toMatchObject({
      speakNextReply: false,
      dictating: false,
    });
  });
});

describe("voiceTranscriptionLanguage", () => {
  it("sends nothing for auto, so the server default decides", () => {
    // Reading the app in English is not a claim about what you speak.
    expect(voiceTranscriptionLanguage("auto")).toBe("");
  });

  it("sends the bare code for an explicit choice", () => {
    expect(voiceTranscriptionLanguage("fr")).toBe("fr");
    expect(voiceTranscriptionLanguage("zh")).toBe("zh");
  });
});

describe("resolveVoiceLocale", () => {
  it("follows the app locale on auto", () => {
    expect(resolveVoiceLocale("auto", "fr")).toBe("fr-FR");
    expect(resolveVoiceLocale("auto", "zh-Hans")).toBe("zh-CN");
    expect(resolveVoiceLocale("auto", "ja")).toBe("ja-JP");
    expect(resolveVoiceLocale("auto", "ko")).toBe("ko-KR");
    expect(resolveVoiceLocale("auto", "en")).toBe("en-US");
  });

  it("overrides the app locale with an explicit choice", () => {
    expect(resolveVoiceLocale("fr", "en")).toBe("fr-FR");
    expect(resolveVoiceLocale("zh", "en")).toBe("zh-CN");
  });

  it("keeps a regioned locale it does not know, and never returns a bare code", () => {
    expect(resolveVoiceLocale("auto", "pt-BR")).toBe("pt-BR");
    expect(resolveVoiceLocale("auto", "sv")).toBe("en-US");
  });
});
