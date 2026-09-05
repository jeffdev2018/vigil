// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { configStore } from "@multica/core/config";
import {
  SERVER_SPEECH_MAX_CHARS,
  clearServerSpeechCache,
  isServerSpeechAvailable,
  playServerSpeech,
} from "./server-speech";
import { splitUtterances } from "./speech-text";

const speak = vi.hoisted(() => vi.fn(async (_t: string, _l?: string) => new Blob(["mp3"])));
vi.mock("@multica/core/api", () => ({ api: { speak } }));

// jsdom has no media pipeline: HTMLMediaElement.play() throws "not
// implemented". Resolve it and fire `ended` so the sequencing is testable.
class FakeAudio {
  src = "";
  onended: (() => void) | null = null;
  onerror: (() => void) | null = null;
  static instances: FakeAudio[] = [];
  constructor() {
    FakeAudio.instances.push(this);
  }
  play(): Promise<void> {
    queueMicrotask(() => this.onended?.());
    return Promise.resolve();
  }
  pause(): void {}
}

beforeEach(() => {
  vi.clearAllMocks();
  clearServerSpeechCache();
  FakeAudio.instances = [];
  vi.stubGlobal("Audio", FakeAudio);
  vi.stubGlobal("URL", {
    createObjectURL: () => "blob:clip",
    revokeObjectURL: () => {},
  });
  configStore.getState().setTtsAvailable(true);
});

afterEach(() => {
  vi.unstubAllGlobals();
  configStore.getState().setTtsAvailable(false);
});

describe("isServerSpeechAvailable", () => {
  it("follows the server capability, and fails closed when it is absent", () => {
    expect(isServerSpeechAvailable()).toBe(true);
    configStore.getState().setTtsAvailable(undefined);
    expect(isServerSpeechAvailable()).toBe(false);
  });
});

describe("playServerSpeech", () => {
  it("plays every chunk in order, then reports the end once", async () => {
    const onEnd = vi.fn();
    const onError = vi.fn();
    playServerSpeech(["un.", "deux."], "fr", onEnd, onError);
    await vi.waitFor(() => expect(onEnd).toHaveBeenCalledTimes(1));
    expect(speak.mock.calls.map(([text]) => text)).toEqual(["un.", "deux."]);
    expect(onError).not.toHaveBeenCalled();
  });

  it("asks the server once for the same text and language", async () => {
    playServerSpeech(["un."], "fr", () => {}, () => {});
    await vi.waitFor(() => expect(speak).toHaveBeenCalledTimes(1));
    playServerSpeech(["un."], "fr", () => {}, () => {});
    await vi.waitFor(() => expect(speak).toHaveBeenCalledTimes(1));
    // A different language is a different clip.
    playServerSpeech(["un."], "ja", () => {}, () => {});
    await vi.waitFor(() => expect(speak).toHaveBeenCalledTimes(2));
  });

  it("reports a failed request so the caller can fall back to the browser voice", async () => {
    speak.mockRejectedValueOnce(new Error("409"));
    const onEnd = vi.fn();
    const onError = vi.fn();
    playServerSpeech(["un."], "fr", onEnd, onError);
    await vi.waitFor(() => expect(onError).toHaveBeenCalledTimes(1));
    expect(onEnd).not.toHaveBeenCalled();
  });

  it("stop() ends the playback and never reports an end or an error", async () => {
    const onEnd = vi.fn();
    const onError = vi.fn();
    const handle = playServerSpeech(["un.", "deux."], "fr", onEnd, onError);
    handle.stop();
    handle.stop(); // idempotent
    await Promise.resolve();
    expect(onEnd).not.toHaveBeenCalled();
    expect(onError).not.toHaveBeenCalled();
  });
});

describe("SERVER_SPEECH_MAX_CHARS", () => {
  // The endpoint refuses anything longer, so the split has to respect it.
  it("bounds every block the caller sends", () => {
    const text = Array.from({ length: 400 }, () => "Une phrase de test.").join(" ");
    for (const block of splitUtterances(text, SERVER_SPEECH_MAX_CHARS)) {
      expect(block.length).toBeLessThanOrEqual(SERVER_SPEECH_MAX_CHARS);
    }
  });
});
