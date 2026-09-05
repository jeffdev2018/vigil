// @vitest-environment node
import { describe, expect, it } from "vitest";
import { speechTextFromMarkdown, splitUtterances } from "./speech-text";

describe("speechTextFromMarkdown", () => {
  it("drops markup that makes no sense aloud and keeps the words", () => {
    const md = [
      "## Résumé",
      "",
      "- **Livraison** du connecteur [Stripe](https://stripe.com) vendredi.",
      "- Voir `config.yaml` et https://example.com/doc",
      "",
      "```ts",
      "const x = 1;",
      "```",
      "",
      "> Note: *important*",
      "",
      "| a | b |",
      "|---|---|",
      "| 1 | 2 |",
    ].join("\n");
    const text = speechTextFromMarkdown(md);
    expect(text).toContain("Résumé");
    expect(text).toContain("Livraison du connecteur Stripe vendredi.");
    expect(text).toContain("Voir config.yaml et");
    expect(text).not.toContain("https://");
    expect(text).not.toContain("const x");
    expect(text).toContain("(code)");
    expect(text).toContain("Note: important");
    expect(text).not.toContain("**");
    expect(text).not.toContain("|");
    expect(text).toContain("1, 2");
  });

  it("returns an empty string for empty input", () => {
    expect(speechTextFromMarkdown("   \n  ")).toBe("");
  });
});

describe("splitUtterances", () => {
  it("groups sentences under the character budget", () => {
    const text = "Première phrase. Deuxième phrase ! Troisième ? Quatrième.";
    expect(splitUtterances(text, 30)).toEqual([
      "Première phrase.",
      "Deuxième phrase ! Troisième ?",
      "Quatrième.",
    ]);
  });

  it("keeps one long sentence whole rather than cutting mid-word", () => {
    const long = "a".repeat(500);
    expect(splitUtterances(long, 100)).toEqual([long]);
  });
});
