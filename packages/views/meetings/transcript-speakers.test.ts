// @vitest-environment node
import { describe, expect, it } from "vitest";
import { hasSpeakers, parseTranscriptBlocks } from "./transcript-speakers";

describe("parseTranscriptBlocks", () => {
  it("splits diarized lines into speaker blocks", () => {
    expect(
      parseTranscriptBlocks("Speaker 1: On livre vendredi.\nSpeaker 2: Ok."),
    ).toEqual([
      { speaker: "Speaker 1", text: "On livre vendredi." },
      { speaker: "Speaker 2", text: "Ok." },
    ]);
  });

  it("joins consecutive lines from the same speaker into one paragraph", () => {
    expect(
      parseTranscriptBlocks(
        "Speaker 1: On livre vendredi.\nSpeaker 1: Enfin, si le CI passe.\nSpeaker 2: Ok.",
      ),
    ).toEqual([
      { speaker: "Speaker 1", text: "On livre vendredi. Enfin, si le CI passe." },
      { speaker: "Speaker 2", text: "Ok." },
    ]);
  });

  it("accepts a provider's own speaker names", () => {
    expect(parseTranscriptBlocks("Paul: je m'en occupe.")).toEqual([
      { speaker: "Paul", text: "je m'en occupe." },
    ]);
  });

  it("leaves the live path's unlabelled text alone, as one block", () => {
    expect(parseTranscriptBlocks("On livre vendredi.\nOk, noté.")).toEqual([
      { speaker: null, text: "On livre vendredi. Ok, noté." },
    ]);
  });

  it("does not mistake a colon inside a sentence for a speaker change", () => {
    for (const line of [
      "On a décidé, finalement: on livre vendredi.",
      "Note. Attention: le CI est rouge.",
      "https://example.test/a",
      "Voilà:",
      ":oups",
    ]) {
      expect(parseTranscriptBlocks(line)).toEqual([{ speaker: null, text: line }]);
    }
  });

  it("refuses a label long enough to be a sentence", () => {
    const long = `${"a".repeat(41)}: du texte`;
    expect(parseTranscriptBlocks(long)).toEqual([{ speaker: null, text: long }]);
  });

  it("drops blank lines and returns nothing for an empty transcript", () => {
    expect(parseTranscriptBlocks("")).toEqual([]);
    expect(parseTranscriptBlocks("\n  \n")).toEqual([]);
  });
});

describe("hasSpeakers", () => {
  it("is true only when something is attributed", () => {
    expect(hasSpeakers(parseTranscriptBlocks("Speaker 1: a"))).toBe(true);
    expect(hasSpeakers(parseTranscriptBlocks("a\nb"))).toBe(false);
    expect(hasSpeakers([])).toBe(false);
  });
});
